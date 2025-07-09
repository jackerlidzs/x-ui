package controller

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"x-ui/logger"
	"x-ui/web/service"
	"x-ui/web/session"
)

// WebSocketController handles WebSocket connections
type WebSocketController struct {
	upgrader websocket.Upgrader
	clients  map[*websocket.Conn]*WebSocketClient
	mu       sync.RWMutex
	
	statsService   service.StatsService
	inboundService service.InboundService
	serverService  service.ServerService
	metricsService *service.MetricsService
	cacheService   *service.CacheService
}

// WebSocketClient represents a connected client
type WebSocketClient struct {
	conn     *websocket.Conn
	send     chan []byte
	userID   int
	clientIP string
}

// WebSocketMessage represents a WebSocket message
type WebSocketMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// DashboardData represents real-time dashboard data
type DashboardData struct {
	TotalUsers        int                    `json:"totalUsers"`
	ActiveConnections int                    `json:"activeConnections"`
	TotalTraffic      int64                  `json:"totalTraffic"`
	SystemLoad        float64                `json:"systemLoad"`
	XRayStatus        bool                   `json:"xrayStatus"`
	InboundStats      []InboundRealTimeStats `json:"inboundStats"`
	TrafficChart      TrafficChartData       `json:"trafficChart"`
	ProtocolUsage     map[string]int         `json:"protocolUsage"`
}

// InboundRealTimeStats represents real-time inbound statistics
type InboundRealTimeStats struct {
	ID                int    `json:"id"`
	Port              int    `json:"port"`
	Protocol          string `json:"protocol"`
	Status            string `json:"status"`
	ConnectedClients  int    `json:"connectedClients"`
	UploadTraffic     int64  `json:"uploadTraffic"`
	DownloadTraffic   int64  `json:"downloadTraffic"`
	TotalTraffic      int64  `json:"totalTraffic"`
	LastActivity      int64  `json:"lastActivity"`
}

// TrafficChartData represents traffic chart data for visualization
type TrafficChartData struct {
	Labels   []string    `json:"labels"`
	Upload   []float64   `json:"upload"`
	Download []float64   `json:"download"`
	Period   string      `json:"period"`
}

// SystemMetrics represents system performance metrics
type SystemMetrics struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskUsage   float64 `json:"diskUsage"`
	NetworkIO   struct {
		BytesIn  int64 `json:"bytesIn"`
		BytesOut int64 `json:"bytesOut"`
	} `json:"networkIO"`
}

// NewWebSocketController creates a new WebSocket controller
func NewWebSocketController(g *gin.RouterGroup) *WebSocketController {
	ws := &WebSocketController{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for development
				// In production, implement proper origin checking
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		clients:        make(map[*websocket.Conn]*WebSocketClient),
		metricsService: service.GetMetricsService(),
		cacheService:   service.GetCacheService(),
	}

	ws.initRouter(g)
	ws.startBroadcaster()
	return ws
}

func (ws *WebSocketController) initRouter(g *gin.RouterGroup) {
	g.GET("/ws", ws.handleWebSocket)
	g.GET("/ws/stats", ws.handleStatsWebSocket)
	g.GET("/ws/logs", ws.handleLogsWebSocket)
}

// handleWebSocket handles the main WebSocket connection
func (ws *WebSocketController) handleWebSocket(c *gin.Context) {
	// Check if user is authenticated
	user := session.GetLoginUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := ws.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("Failed to upgrade to WebSocket:", err)
		return
	}

	client := &WebSocketClient{
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   user.Id,
		clientIP: c.ClientIP(),
	}

	ws.addClient(conn, client)
	logger.Info("New WebSocket client connected:", client.clientIP)

	// Track metrics
	ws.metricsService.UpdateOnlineUsers(float64(len(ws.clients)))

	// Start client handler
	go ws.handleClient(client)
	go ws.writePump(client)
}

// handleStatsWebSocket handles statistics-specific WebSocket connections
func (ws *WebSocketController) handleStatsWebSocket(c *gin.Context) {
	user := session.GetLoginUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	conn, err := ws.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("Failed to upgrade stats WebSocket:", err)
		return
	}

	client := &WebSocketClient{
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   user.Id,
		clientIP: c.ClientIP(),
	}

	ws.addClient(conn, client)

	// Send initial stats data
	go func() {
		time.Sleep(time.Second) // Wait for connection to stabilize
		ws.sendStatsUpdate(client)
	}()

	go ws.handleClient(client)
	go ws.writePump(client)
}

// handleLogsWebSocket handles log streaming WebSocket connections
func (ws *WebSocketController) handleLogsWebSocket(c *gin.Context) {
	user := session.GetLoginUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	conn, err := ws.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("Failed to upgrade logs WebSocket:", err)
		return
	}

	client := &WebSocketClient{
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   user.Id,
		clientIP: c.ClientIP(),
	}

	ws.addClient(conn, client)
	go ws.handleClient(client)
	go ws.writePump(client)
}

// addClient adds a new client to the clients map
func (ws *WebSocketController) addClient(conn *websocket.Conn, client *WebSocketClient) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.clients[conn] = client
}

// removeClient removes a client from the clients map
func (ws *WebSocketController) removeClient(conn *websocket.Conn) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	
	if client, ok := ws.clients[conn]; ok {
		close(client.send)
		delete(ws.clients, conn)
		conn.Close()
		
		// Update metrics
		ws.metricsService.UpdateOnlineUsers(float64(len(ws.clients)))
		logger.Info("WebSocket client disconnected:", client.clientIP)
	}
}

// handleClient handles individual client messages
func (ws *WebSocketController) handleClient(client *WebSocketClient) {
	defer ws.removeClient(client.conn)

	client.conn.SetReadLimit(512)
	client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("WebSocket error:", err)
			}
			break
		}

		// Handle incoming message
		var msg WebSocketMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Error("Failed to unmarshal WebSocket message:", err)
			continue
		}

		ws.handleMessage(client, &msg)
	}
}

// writePump pumps messages from the send channel to the WebSocket connection
func (ws *WebSocketController) writePump(client *WebSocketClient) {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(client.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-client.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming WebSocket messages
func (ws *WebSocketController) handleMessage(client *WebSocketClient, msg *WebSocketMessage) {
	switch msg.Type {
	case "subscribe_dashboard":
		// Client wants to subscribe to dashboard updates
		ws.sendDashboardUpdate(client)
	case "subscribe_stats":
		// Client wants to subscribe to stats updates
		ws.sendStatsUpdate(client)
	case "subscribe_logs":
		// Client wants to subscribe to log updates
		ws.sendLogsUpdate(client)
	case "get_inbound_details":
		// Client requests detailed inbound information
		if inboundID, ok := msg.Data.(float64); ok {
			ws.sendInboundDetails(client, int(inboundID))
		}
	case "ping":
		// Respond to ping
		ws.sendMessage(client, "pong", "pong")
	default:
		logger.Warning("Unknown WebSocket message type:", msg.Type)
	}
}

// startBroadcaster starts the background broadcaster for real-time updates
func (ws *WebSocketController) startBroadcaster() {
	// Dashboard updates every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			ws.broadcastDashboardUpdate()
		}
	}()

	// Stats updates every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			ws.broadcastStatsUpdate()
		}
	}()

	// System metrics every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			ws.broadcastSystemMetrics()
		}
	}()
}

// broadcastDashboardUpdate broadcasts dashboard updates to all connected clients
func (ws *WebSocketController) broadcastDashboardUpdate() {
	data := ws.getDashboardData()
	message := WebSocketMessage{
		Type:      "dashboard_update",
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	ws.broadcast(message)
}

// broadcastStatsUpdate broadcasts statistics updates
func (ws *WebSocketController) broadcastStatsUpdate() {
	stats := ws.getInboundStats()
	message := WebSocketMessage{
		Type:      "stats_update",
		Data:      stats,
		Timestamp: time.Now().Unix(),
	}

	ws.broadcast(message)
}

// broadcastSystemMetrics broadcasts system metrics
func (ws *WebSocketController) broadcastSystemMetrics() {
	metrics := ws.getSystemMetrics()
	message := WebSocketMessage{
		Type:      "system_metrics",
		Data:      metrics,
		Timestamp: time.Now().Unix(),
	}

	ws.broadcast(message)
}

// broadcast sends a message to all connected clients
func (ws *WebSocketController) broadcast(message WebSocketMessage) {
	messageBytes, err := json.Marshal(message)
	if err != nil {
		logger.Error("Failed to marshal WebSocket message:", err)
		return
	}

	ws.mu.RLock()
	defer ws.mu.RUnlock()

	for conn, client := range ws.clients {
		select {
		case client.send <- messageBytes:
		default:
			close(client.send)
			delete(ws.clients, conn)
		}
	}
}

// sendMessage sends a message to a specific client
func (ws *WebSocketController) sendMessage(client *WebSocketClient, msgType string, data interface{}) {
	message := WebSocketMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		logger.Error("Failed to marshal WebSocket message:", err)
		return
	}

	select {
	case client.send <- messageBytes:
	default:
		close(client.send)
		ws.removeClient(client.conn)
	}
}

// sendDashboardUpdate sends dashboard update to a specific client
func (ws *WebSocketController) sendDashboardUpdate(client *WebSocketClient) {
	data := ws.getDashboardData()
	ws.sendMessage(client, "dashboard_update", data)
}

// sendStatsUpdate sends stats update to a specific client
func (ws *WebSocketController) sendStatsUpdate(client *WebSocketClient) {
	stats := ws.getInboundStats()
	ws.sendMessage(client, "stats_update", stats)
}

// sendLogsUpdate sends log update to a specific client
func (ws *WebSocketController) sendLogsUpdate(client *WebSocketClient) {
	// Implementation for log streaming would go here
	logs := []string{"Sample log entry", "Another log entry"}
	ws.sendMessage(client, "logs_update", logs)
}

// sendInboundDetails sends detailed inbound information to a client
func (ws *WebSocketController) sendInboundDetails(client *WebSocketClient, inboundID int) {
	// Get detailed inbound information
	inbound, err := ws.inboundService.GetInbound(inboundID)
	if err != nil {
		ws.sendMessage(client, "error", "Failed to get inbound details")
		return
	}

	ws.sendMessage(client, "inbound_details", inbound)
}

// getDashboardData collects dashboard data
func (ws *WebSocketController) getDashboardData() DashboardData {
	// Try to get from cache first
	var data DashboardData
	if ws.cacheService.IsEnabled() {
		if err := ws.cacheService.GetSystemStats(&data); err == nil {
			return data
		}
	}

	// Collect fresh data
	data = DashboardData{
		TotalUsers:        ws.getTotalUsers(),
		ActiveConnections: ws.getActiveConnections(),
		TotalTraffic:      ws.getTotalTraffic(),
		SystemLoad:        ws.getSystemLoad(),
		XRayStatus:        ws.getXRayStatus(),
		InboundStats:      ws.getInboundStatsReal(),
		TrafficChart:      ws.getTrafficChartData(),
		ProtocolUsage:     ws.getProtocolUsage(),
	}

	// Cache the data
	if ws.cacheService.IsEnabled() {
		ws.cacheService.SetSystemStats(data)
	}

	return data
}

// Helper methods to collect specific data
func (ws *WebSocketController) getTotalUsers() int {
	// Implementation to get total users
	return 0 // Placeholder
}

func (ws *WebSocketController) getActiveConnections() int {
	return len(ws.clients)
}

func (ws *WebSocketController) getTotalTraffic() int64 {
	// Implementation to get total traffic
	return 0 // Placeholder
}

func (ws *WebSocketController) getSystemLoad() float64 {
	// Implementation to get system load
	return 0.0 // Placeholder
}

func (ws *WebSocketController) getXRayStatus() bool {
	// Implementation to check XRay status
	return true // Placeholder
}

func (ws *WebSocketController) getInboundStatsReal() []InboundRealTimeStats {
	// Implementation to get real-time inbound stats
	return []InboundRealTimeStats{} // Placeholder
}

func (ws *WebSocketController) getInboundStats() interface{} {
	// Implementation to get inbound statistics
	return map[string]interface{}{
		"total":    0,
		"active":   0,
		"inactive": 0,
	}
}

func (ws *WebSocketController) getTrafficChartData() TrafficChartData {
	// Implementation to get traffic chart data
	return TrafficChartData{
		Labels:   []string{"00:00", "04:00", "08:00", "12:00", "16:00", "20:00"},
		Upload:   []float64{12, 19, 3, 5, 2, 3},
		Download: []float64{7, 11, 5, 8, 3, 7},
		Period:   "24h",
	}
}

func (ws *WebSocketController) getProtocolUsage() map[string]int {
	// Implementation to get protocol usage statistics
	return map[string]int{
		"vmess":       45,
		"vless":       30,
		"trojan":      15,
		"shadowsocks": 10,
	}
}

func (ws *WebSocketController) getSystemMetrics() SystemMetrics {
	// Implementation to get system metrics
	return SystemMetrics{
		CPUUsage:    25.5,
		MemoryUsage: 60.2,
		DiskUsage:   45.8,
		NetworkIO: struct {
			BytesIn  int64 `json:"bytesIn"`
			BytesOut int64 `json:"bytesOut"`
		}{
			BytesIn:  1024000,
			BytesOut: 2048000,
		},
	}
}

// GetConnectedClients returns the number of connected WebSocket clients
func (ws *WebSocketController) GetConnectedClients() int {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return len(ws.clients)
}

// DisconnectClient disconnects a specific client
func (ws *WebSocketController) DisconnectClient(conn *websocket.Conn) {
	ws.removeClient(conn)
}

// BroadcastMessage broadcasts a custom message to all clients
func (ws *WebSocketController) BroadcastMessage(msgType string, data interface{}) {
	message := WebSocketMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	ws.broadcast(message)
}