package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"x-ui/database/model"
	"x-ui/logger"
	"x-ui/web/global"
	"x-ui/web/middleware"
	"x-ui/web/service"
	"x-ui/web/session"
)

type InboundController struct {
	inboundService service.InboundService
	xrayService    service.XrayService
	cacheService   *service.CacheService
	metricsService *service.MetricsService
}

func NewInboundController(g *gin.RouterGroup) *InboundController {
	a := &InboundController{
		cacheService:   service.GetCacheService(),
		metricsService: service.GetMetricsService(),
	}
	a.initRouter(g)
	a.startTask()
	return a
}

func (a *InboundController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/inbound")

	// Apply rate limiting middleware
	g.Use(middleware.InboundRateLimit())
	g.Use(middleware.ValidationMiddleware())

	g.POST("/list", a.getInbounds)
	g.POST("/add", a.addInbound)
	g.POST("/del/:id", a.delInbound)
	g.POST("/update/:id", a.updateInbound)

	g.POST("/clientIps/:email", a.getClientIps)
	g.POST("/clearClientIps/:email", a.clearClientIps)
	g.POST("/resetClientTraffic/:email", a.resetClientTraffic)
	
	// New enhanced endpoints
	g.GET("/stats/:id", a.getInboundStats)
	g.POST("/toggle/:id", a.toggleInbound)
	g.GET("/export/:id", a.exportInbound)
	g.POST("/import", a.importInbound)
}

func (a *InboundController) startTask() {
	webServer := global.GetWebServer()
	c := webServer.GetCron()
	c.AddFunc("@every 10s", func() {
		if a.xrayService.IsNeedRestartAndSetFalse() {
			err := a.xrayService.RestartXray(false)
			if err != nil {
				logger.Error("restart xray failed:", err)
			}
		}
	})
}

func (a *InboundController) getInbounds(c *gin.Context) {
	startTime := time.Now()
	user := session.GetLoginUser(c)
	
	// Try to get from cache first
	cacheKey := fmt.Sprintf("user_inbounds:%d", user.Id)
	var inbounds []*model.Inbound
	
	if a.cacheService.IsEnabled() {
		err := a.cacheService.Get(cacheKey, &inbounds)
		if err == nil {
			a.metricsService.TrackCacheOperation("inbounds", true)
			a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/list", http.StatusOK, time.Since(startTime), 0)
			jsonObj(c, inbounds, nil)
			return
		}
		a.metricsService.TrackCacheOperation("inbounds", false)
	}

	// Get from database
	inbounds, err := a.inboundService.GetInbounds(user.Id)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/list", http.StatusInternalServerError, time.Since(startTime), 0)
		jsonMsg(c, I18n(c , "pages.inbounds.toasts.obtain"), err)
		return
	}

	// Cache the result
	if a.cacheService.IsEnabled() {
		a.cacheService.Set(cacheKey, inbounds, service.ShortTTL)
	}

	// Update metrics
	a.metricsService.UpdateInboundCount("total", "active", float64(len(inbounds)))
	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/list", http.StatusOK, time.Since(startTime), 0)
	
	jsonObj(c, inbounds, nil)
}
func (a *InboundController) getInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18n(c , "get"), err)
		return
	}
	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		jsonMsg(c, I18n(c , "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, inbound, nil)
}

func (a *InboundController) addInbound(c *gin.Context) {
	startTime := time.Now()
	
	// Validate input using middleware validator
	validator, err := middleware.ValidateInbound(c)
	if err != nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/add", http.StatusBadRequest, time.Since(startTime), 0)
		return // Error response already sent by validator
	}

	user := session.GetLoginUser(c)
	
	// Convert validator to model
	inbound := &model.Inbound{
		UserId:         user.Id,
		Port:           validator.Port,
		Protocol:       model.Protocol(validator.Protocol),
		Remark:         middleware.SanitizeInput(validator.Remark),
		Enable:         true,
		Listen:         validator.Listen,
		Settings:       validator.Settings,
		StreamSettings: validator.StreamSettings,
		Sniffing:       validator.Sniffing,
		Tag:            fmt.Sprintf("inbound-%v", validator.Port),
	}

	inbound, err = a.inboundService.AddInbound(inbound)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/add", http.StatusInternalServerError, time.Since(startTime), 0)
		jsonMsg(c, I18n(c , "pages.inbounds.addTo"), err)
		return
	}

	// Invalidate cache
	if a.cacheService.IsEnabled() {
		cacheKey := fmt.Sprintf("user_inbounds:%d", user.Id)
		a.cacheService.Delete(cacheKey)
	}

	// Update metrics
	a.metricsService.UpdateInboundCount(validator.Protocol, "active", 1)
	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/add", http.StatusOK, time.Since(startTime), 0)

	jsonMsgObj(c, I18n(c , "pages.inbounds.addTo"), inbound, nil)
	
	if err == nil {
		a.xrayService.SetToNeedRestart()
		logger.Info(fmt.Sprintf("User %d added new inbound %d (%s:%d)", user.Id, inbound.Id, inbound.Protocol, inbound.Port))
	}
}

func (a *InboundController) delInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18n(c , "delete"), err)
		return
	}
	err = a.inboundService.DelInbound(id)
	jsonMsgObj(c, I18n(c , "delete"), id, err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *InboundController) updateInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18n(c , "pages.inbounds.revise"), err)
		return
	}
	inbound := &model.Inbound{
		Id: id,
	}
	err = c.ShouldBind(inbound)
	if err != nil {
		jsonMsg(c, I18n(c , "pages.inbounds.revise"), err)
		return
	}
	inbound, err = a.inboundService.UpdateInbound(inbound)
	jsonMsgObj(c, I18n(c , "pages.inbounds.revise"), inbound, err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}
func (a *InboundController) getClientIps(c *gin.Context) {
	email := c.Param("email")

	ips , err := a.inboundService.GetInboundClientIps(email)
	if err != nil {
		jsonObj(c, "No IP Record", nil)
		return
	}
	jsonObj(c, ips, nil)
}
func (a *InboundController) clearClientIps(c *gin.Context) {
	email := c.Param("email")

	err := a.inboundService.ClearClientIps(email)
	if err != nil {
		jsonMsg(c, "修改", err)
		return
	}
	jsonMsg(c, "Log Cleared", nil)
}
func (a *InboundController) resetClientTraffic(c *gin.Context) {
	email := c.Param("email")

	err := a.inboundService.ResetClientTraffic(email)
	if err != nil {
		jsonMsg(c, "something worng!", err)
		return
	}
	jsonMsg(c, "traffic reseted", nil)
}

// getInboundStats returns detailed statistics for a specific inbound
func (a *InboundController) getInboundStats(c *gin.Context) {
	startTime := time.Now()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/stats", http.StatusBadRequest, time.Since(startTime), 0)
		jsonMsg(c, "Invalid inbound ID", err)
		return
	}

	// Try cache first
	if a.cacheService.IsEnabled() {
		stats, err := a.cacheService.GetInboundStats(id)
		if err == nil && stats != nil {
			a.metricsService.TrackCacheOperation("inbound_stats", true)
			a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/stats", http.StatusOK, time.Since(startTime), 0)
			jsonObj(c, stats, nil)
			return
		}
		a.metricsService.TrackCacheOperation("inbound_stats", false)
	}

	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/stats", http.StatusNotFound, time.Since(startTime), 0)
		jsonMsg(c, "Inbound not found", err)
		return
	}

	// Cache the result
	if a.cacheService.IsEnabled() {
		a.cacheService.SetInboundStats(id, inbound)
	}

	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/stats", http.StatusOK, time.Since(startTime), 0)
	jsonObj(c, inbound, nil)
}

// toggleInbound toggles the enable/disable status of an inbound
func (a *InboundController) toggleInbound(c *gin.Context) {
	startTime := time.Now()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/toggle", http.StatusBadRequest, time.Since(startTime), 0)
		jsonMsg(c, "Invalid inbound ID", err)
		return
	}

	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/toggle", http.StatusNotFound, time.Since(startTime), 0)
		jsonMsg(c, "Inbound not found", err)
		return
	}

	// Toggle the enable status
	inbound.Enable = !inbound.Enable
	
	inbound, err = a.inboundService.UpdateInbound(inbound)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/toggle", http.StatusInternalServerError, time.Since(startTime), 0)
		jsonMsg(c, "Failed to toggle inbound", err)
		return
	}

	// Invalidate cache
	if a.cacheService.IsEnabled() {
		a.cacheService.InvalidateInbound(id)
		user := session.GetLoginUser(c)
		cacheKey := fmt.Sprintf("user_inbounds:%d", user.Id)
		a.cacheService.Delete(cacheKey)
	}

	// Update metrics
	status := "active"
	if !inbound.Enable {
		status = "inactive"
	}
	a.metricsService.UpdateInboundCount(string(inbound.Protocol), status, 1)
	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/toggle", http.StatusOK, time.Since(startTime), 0)

	a.xrayService.SetToNeedRestart()
	jsonMsgObj(c, "Inbound status toggled", inbound, nil)
}

// exportInbound exports inbound configuration
func (a *InboundController) exportInbound(c *gin.Context) {
	startTime := time.Now()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/export", http.StatusBadRequest, time.Since(startTime), 0)
		jsonMsg(c, "Invalid inbound ID", err)
		return
	}

	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/export", http.StatusNotFound, time.Since(startTime), 0)
		jsonMsg(c, "Inbound not found", err)
		return
	}

	// Create export data structure
	exportData := struct {
		*model.Inbound
		ExportedAt int64  `json:"exportedAt"`
		Version    string `json:"version"`
	}{
		Inbound:    inbound,
		ExportedAt: time.Now().Unix(),
		Version:    "1.0",
	}

	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/export", http.StatusOK, time.Since(startTime), 0)
	
	// Set headers for file download
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=inbound_%d.json", id))
	
	c.JSON(http.StatusOK, exportData)
}

// importInbound imports inbound configuration
func (a *InboundController) importInbound(c *gin.Context) {
	startTime := time.Now()
	user := session.GetLoginUser(c)

	var importData struct {
		*model.Inbound
		ExportedAt int64  `json:"exportedAt"`
		Version    string `json:"version"`
	}

	if err := c.ShouldBindJSON(&importData); err != nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/import", http.StatusBadRequest, time.Since(startTime), 0)
		jsonMsg(c, "Invalid import data", err)
		return
	}

	// Validate imported data
	if importData.Inbound == nil {
		a.metricsService.IncrementErrors("validation", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/import", http.StatusBadRequest, time.Since(startTime), 0)
		jsonMsg(c, "No inbound data found", nil)
		return
	}

	// Reset ID and set user
	importData.Inbound.Id = 0
	importData.Inbound.UserId = user.Id
	importData.Inbound.Tag = fmt.Sprintf("inbound-%v", importData.Inbound.Port)

	// Sanitize input
	importData.Inbound.Remark = middleware.SanitizeInput(importData.Inbound.Remark)

	inbound, err := a.inboundService.AddInbound(importData.Inbound)
	if err != nil {
		a.metricsService.IncrementErrors("database", "inbound_controller")
		a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/import", http.StatusInternalServerError, time.Since(startTime), 0)
		jsonMsg(c, "Failed to import inbound", err)
		return
	}

	// Invalidate cache
	if a.cacheService.IsEnabled() {
		cacheKey := fmt.Sprintf("user_inbounds:%d", user.Id)
		a.cacheService.Delete(cacheKey)
	}

	// Update metrics
	a.metricsService.UpdateInboundCount(string(inbound.Protocol), "active", 1)
	a.metricsService.TrackHTTPRequest(c.Request.Method, "/inbound/import", http.StatusOK, time.Since(startTime), 0)

	a.xrayService.SetToNeedRestart()
	logger.Info(fmt.Sprintf("User %d imported inbound %d (%s:%d)", user.Id, inbound.Id, inbound.Protocol, inbound.Port))
	
	jsonMsgObj(c, "Inbound imported successfully", inbound, nil)
}
