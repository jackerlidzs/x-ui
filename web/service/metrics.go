package service

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"x-ui/logger"
)

// MetricsService handles all metrics collection and reporting
type MetricsService struct {
	// System metrics
	SystemCPUUsage    prometheus.Gauge
	SystemMemoryUsage prometheus.Gauge
	SystemDiskUsage   prometheus.Gauge
	SystemUptime      prometheus.Gauge

	// Application metrics
	ActiveConnections   prometheus.Gauge
	InboundCount        prometheus.GaugeVec
	UserCount          prometheus.Gauge
	OnlineUsers        prometheus.Gauge

	// Traffic metrics
	TrafficBytesTotal    prometheus.CounterVec
	TrafficBandwidth     prometheus.GaugeVec
	ClientConnections    prometheus.GaugeVec

	// HTTP metrics
	HTTPRequestsTotal     prometheus.CounterVec
	HTTPRequestDuration   prometheus.HistogramVec
	HTTPResponseSize      prometheus.HistogramVec

	// Rate limiting metrics
	RateLimitHits         prometheus.CounterVec
	RateLimitBlocked      prometheus.CounterVec

	// Cache metrics
	CacheHits             prometheus.CounterVec
	CacheMisses           prometheus.CounterVec
	CacheSize             prometheus.Gauge

	// Error metrics
	ErrorsTotal           prometheus.CounterVec
	PanicTotal            prometheus.Counter

	// XRay specific metrics
	XRayStatus            prometheus.Gauge
	XRayRestarts          prometheus.Counter
	ProxyConnections      prometheus.GaugeVec
}

var metricsService *MetricsService

// NewMetricsService creates a new metrics service
func NewMetricsService() *MetricsService {
	if metricsService != nil {
		return metricsService
	}

	ms := &MetricsService{
		// System metrics
		SystemCPUUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_system_cpu_usage_percent",
			Help: "Current CPU usage percentage",
		}),
		SystemMemoryUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_system_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		}),
		SystemDiskUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_system_disk_usage_bytes",
			Help: "Current disk usage in bytes",
		}),
		SystemUptime: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_system_uptime_seconds",
			Help: "System uptime in seconds",
		}),

		// Application metrics
		ActiveConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_active_connections_total",
			Help: "Number of active connections",
		}),
		InboundCount: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "xui_inbounds_total",
			Help: "Total number of inbounds by protocol and status",
		}, []string{"protocol", "status"}),
		UserCount: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_users_total",
			Help: "Total number of users",
		}),
		OnlineUsers: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_online_users_total",
			Help: "Number of currently online users",
		}),

		// Traffic metrics
		TrafficBytesTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_traffic_bytes_total",
			Help: "Total traffic in bytes",
		}, []string{"direction", "inbound_id", "protocol"}),
		TrafficBandwidth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "xui_traffic_bandwidth_bytes_per_second",
			Help: "Current bandwidth usage in bytes per second",
		}, []string{"direction", "inbound_id"}),
		ClientConnections: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "xui_client_connections_total",
			Help: "Number of client connections per inbound",
		}, []string{"inbound_id", "client_email"}),

		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_http_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"method", "endpoint", "status"}),
		HTTPRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "xui_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "endpoint"}),
		HTTPResponseSize: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "xui_http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: []float64{100, 1000, 10000, 100000, 1000000},
		}, []string{"method", "endpoint"}),

		// Rate limiting metrics
		RateLimitHits: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		}, []string{"limit_type", "client_ip"}),
		RateLimitBlocked: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_rate_limit_blocked_total",
			Help: "Total number of blocked requests due to rate limiting",
		}, []string{"limit_type", "client_ip"}),

		// Cache metrics
		CacheHits: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_cache_hits_total",
			Help: "Total number of cache hits",
		}, []string{"cache_type"}),
		CacheMisses: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_cache_misses_total",
			Help: "Total number of cache misses",
		}, []string{"cache_type"}),
		CacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_cache_size_bytes",
			Help: "Current cache size in bytes",
		}),

		// Error metrics
		ErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xui_errors_total",
			Help: "Total number of errors",
		}, []string{"type", "source"}),
		PanicTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "xui_panics_total",
			Help: "Total number of panics",
		}),

		// XRay specific metrics
		XRayStatus: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "xui_xray_status",
			Help: "XRay service status (1 = running, 0 = stopped)",
		}),
		XRayRestarts: promauto.NewCounter(prometheus.CounterOpts{
			Name: "xui_xray_restarts_total",
			Help: "Total number of XRay restarts",
		}),
		ProxyConnections: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "xui_proxy_connections_total",
			Help: "Number of proxy connections by protocol",
		}, []string{"protocol", "inbound_id"}),
	}

	metricsService = ms
	logger.Info("Metrics service initialized successfully")
	return ms
}

// GetMetricsService returns the global metrics service instance
func GetMetricsService() *MetricsService {
	if metricsService == nil {
		return NewMetricsService()
	}
	return metricsService
}

// System metrics methods

// UpdateSystemCPU updates CPU usage metric
func (m *MetricsService) UpdateSystemCPU(usage float64) {
	m.SystemCPUUsage.Set(usage)
}

// UpdateSystemMemory updates memory usage metric
func (m *MetricsService) UpdateSystemMemory(usage float64) {
	m.SystemMemoryUsage.Set(usage)
}

// UpdateSystemDisk updates disk usage metric
func (m *MetricsService) UpdateSystemDisk(usage float64) {
	m.SystemDiskUsage.Set(usage)
}

// UpdateSystemUptime updates system uptime metric
func (m *MetricsService) UpdateSystemUptime(uptime float64) {
	m.SystemUptime.Set(uptime)
}

// Application metrics methods

// UpdateActiveConnections updates active connections count
func (m *MetricsService) UpdateActiveConnections(count float64) {
	m.ActiveConnections.Set(count)
}

// UpdateInboundCount updates inbound count by protocol and status
func (m *MetricsService) UpdateInboundCount(protocol, status string, count float64) {
	m.InboundCount.WithLabelValues(protocol, status).Set(count)
}

// UpdateUserCount updates total user count
func (m *MetricsService) UpdateUserCount(count float64) {
	m.UserCount.Set(count)
}

// UpdateOnlineUsers updates online users count
func (m *MetricsService) UpdateOnlineUsers(count float64) {
	m.OnlineUsers.Set(count)
}

// Traffic metrics methods

// IncrementTrafficBytes increments traffic bytes counter
func (m *MetricsService) IncrementTrafficBytes(direction, inboundID, protocol string, bytes float64) {
	m.TrafficBytesTotal.WithLabelValues(direction, inboundID, protocol).Add(bytes)
}

// UpdateTrafficBandwidth updates bandwidth usage
func (m *MetricsService) UpdateTrafficBandwidth(direction, inboundID string, bandwidth float64) {
	m.TrafficBandwidth.WithLabelValues(direction, inboundID).Set(bandwidth)
}

// UpdateClientConnections updates client connections count
func (m *MetricsService) UpdateClientConnections(inboundID, clientEmail string, count float64) {
	m.ClientConnections.WithLabelValues(inboundID, clientEmail).Set(count)
}

// HTTP metrics methods

// IncrementHTTPRequests increments HTTP requests counter
func (m *MetricsService) IncrementHTTPRequests(method, endpoint, status string) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
}

// ObserveHTTPDuration observes HTTP request duration
func (m *MetricsService) ObserveHTTPDuration(method, endpoint string, duration time.Duration) {
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// ObserveHTTPResponseSize observes HTTP response size
func (m *MetricsService) ObserveHTTPResponseSize(method, endpoint string, size float64) {
	m.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(size)
}

// Rate limiting metrics methods

// IncrementRateLimitHits increments rate limit hits counter
func (m *MetricsService) IncrementRateLimitHits(limitType, clientIP string) {
	m.RateLimitHits.WithLabelValues(limitType, clientIP).Inc()
}

// IncrementRateLimitBlocked increments rate limit blocked counter
func (m *MetricsService) IncrementRateLimitBlocked(limitType, clientIP string) {
	m.RateLimitBlocked.WithLabelValues(limitType, clientIP).Inc()
}

// Cache metrics methods

// IncrementCacheHits increments cache hits counter
func (m *MetricsService) IncrementCacheHits(cacheType string) {
	m.CacheHits.WithLabelValues(cacheType).Inc()
}

// IncrementCacheMisses increments cache misses counter
func (m *MetricsService) IncrementCacheMisses(cacheType string) {
	m.CacheMisses.WithLabelValues(cacheType).Inc()
}

// UpdateCacheSize updates cache size
func (m *MetricsService) UpdateCacheSize(size float64) {
	m.CacheSize.Set(size)
}

// Error metrics methods

// IncrementErrors increments error counter
func (m *MetricsService) IncrementErrors(errorType, source string) {
	m.ErrorsTotal.WithLabelValues(errorType, source).Inc()
}

// IncrementPanics increments panic counter
func (m *MetricsService) IncrementPanics() {
	m.PanicTotal.Inc()
}

// XRay metrics methods

// UpdateXRayStatus updates XRay service status
func (m *MetricsService) UpdateXRayStatus(running bool) {
	if running {
		m.XRayStatus.Set(1)
	} else {
		m.XRayStatus.Set(0)
	}
}

// IncrementXRayRestarts increments XRay restarts counter
func (m *MetricsService) IncrementXRayRestarts() {
	m.XRayRestarts.Inc()
}

// UpdateProxyConnections updates proxy connections count
func (m *MetricsService) UpdateProxyConnections(protocol, inboundID string, count float64) {
	m.ProxyConnections.WithLabelValues(protocol, inboundID).Set(count)
}

// Utility methods

// ResetInboundMetrics resets all metrics for a specific inbound
func (m *MetricsService) ResetInboundMetrics(inboundID string) {
	// Reset traffic metrics
	m.TrafficBandwidth.DeleteLabelValues("upload", inboundID)
	m.TrafficBandwidth.DeleteLabelValues("download", inboundID)
	
	// Reset proxy connections
	protocols := []string{"vmess", "vless", "trojan", "shadowsocks"}
	for _, protocol := range protocols {
		m.ProxyConnections.DeleteLabelValues(protocol, inboundID)
	}
}

// GetMetricValue gets current value of a metric (for testing/monitoring)
func (m *MetricsService) GetMetricValue(metricName string) (float64, error) {
	metric := prometheus.DefaultGatherer
	families, err := metric.Gather()
	if err != nil {
		return 0, err
	}

	for _, family := range families {
		if *family.Name == metricName {
			if len(family.Metric) > 0 {
				switch family.GetType() {
				case prometheus.MetricType_GAUGE:
					return family.Metric[0].GetGauge().GetValue(), nil
				case prometheus.MetricType_COUNTER:
					return family.Metric[0].GetCounter().GetValue(), nil
				}
			}
		}
	}

	return 0, nil
}

// CollectSystemMetrics collects system-level metrics
func (m *MetricsService) CollectSystemMetrics() {
	// This would typically integrate with system monitoring libraries
	// For now, we'll provide the interface
	// Implementation would depend on the specific system monitoring library used
}

// Helper functions for common metric patterns

// TrackHTTPRequest is a helper to track HTTP request metrics
func (m *MetricsService) TrackHTTPRequest(method, endpoint string, statusCode int, duration time.Duration, responseSize int) {
	status := strconv.Itoa(statusCode)
	m.IncrementHTTPRequests(method, endpoint, status)
	m.ObserveHTTPDuration(method, endpoint, duration)
	m.ObserveHTTPResponseSize(method, endpoint, float64(responseSize))
}

// TrackCacheOperation tracks cache hit/miss
func (m *MetricsService) TrackCacheOperation(cacheType string, hit bool) {
	if hit {
		m.IncrementCacheHits(cacheType)
	} else {
		m.IncrementCacheMisses(cacheType)
	}
}

// TrackTrafficStats tracks traffic statistics
func (m *MetricsService) TrackTrafficStats(inboundID string, protocol string, upload, download int64) {
	idStr := strconv.Itoa(inboundID)
	m.IncrementTrafficBytes("upload", idStr, protocol, float64(upload))
	m.IncrementTrafficBytes("download", idStr, protocol, float64(download))
}

// ExportMetrics exports current metrics for external monitoring
func (m *MetricsService) ExportMetrics() (map[string]interface{}, error) {
	metrics := make(map[string]interface{})
	
	// Get current metric values
	gatherer := prometheus.DefaultGatherer
	families, err := gatherer.Gather()
	if err != nil {
		return nil, err
	}

	for _, family := range families {
		familyName := *family.Name
		familyMetrics := make([]map[string]interface{}, 0)

		for _, metric := range family.Metric {
			metricData := make(map[string]interface{})
			
			// Add labels
			if len(metric.Label) > 0 {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[*label.Name] = *label.Value
				}
				metricData["labels"] = labels
			}

			// Add value based on metric type
			switch family.GetType() {
			case prometheus.MetricType_GAUGE:
				metricData["value"] = metric.GetGauge().GetValue()
			case prometheus.MetricType_COUNTER:
				metricData["value"] = metric.GetCounter().GetValue()
			case prometheus.MetricType_HISTOGRAM:
				metricData["sample_count"] = metric.GetHistogram().GetSampleCount()
				metricData["sample_sum"] = metric.GetHistogram().GetSampleSum()
			}

			familyMetrics = append(familyMetrics, metricData)
		}

		metrics[familyName] = familyMetrics
	}

	return metrics, nil
}