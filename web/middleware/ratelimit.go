package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"x-ui/logger"
)

// RateLimiter represents a rate limiter
type RateLimiter struct {
	mu        sync.RWMutex
	clients   map[string]*Client
	rate      int           // requests per period
	period    time.Duration // time period
	cleanupInterval time.Duration
}

// Client represents a client with rate limiting info
type Client struct {
	tokens    int
	lastReset time.Time
	blocked   bool
	blockUntil time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rate int, period time.Duration) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*Client),
		rate:    rate,
		period:  period,
		cleanupInterval: time.Minute * 10, // cleanup every 10 minutes
	}

	// Start cleanup routine
	go rl.cleanup()

	return rl
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	client, exists := rl.clients[clientID]

	if !exists {
		client = &Client{
			tokens:    rl.rate - 1,
			lastReset: now,
			blocked:   false,
		}
		rl.clients[clientID] = client
		return true
	}

	// Check if client is still blocked
	if client.blocked && now.Before(client.blockUntil) {
		return false
	}

	// Reset tokens if period has passed
	if now.Sub(client.lastReset) >= rl.period {
		client.tokens = rl.rate
		client.lastReset = now
		client.blocked = false
	}

	// Check if tokens available
	if client.tokens > 0 {
		client.tokens--
		return true
	}

	// Block client for a period
	client.blocked = true
	client.blockUntil = now.Add(rl.period)
	
	logger.Warning(fmt.Sprintf("Rate limit exceeded for client: %s", clientID))
	return false
}

// GetClientInfo returns client rate limit info
func (rl *RateLimiter) GetClientInfo(clientID string) *Client {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	if client, exists := rl.clients[clientID]; exists {
		return client
	}
	return nil
}

// cleanup removes old clients
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for clientID, client := range rl.clients {
				// Remove clients that haven't been active for 1 hour
				if now.Sub(client.lastReset) > time.Hour {
					delete(rl.clients, clientID)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Global rate limiters for different endpoints
var (
	// General API rate limiter - 100 requests per minute
	generalLimiter = NewRateLimiter(100, time.Minute)
	
	// Login rate limiter - 5 attempts per 5 minutes
	loginLimiter = NewRateLimiter(5, time.Minute*5)
	
	// Inbound management rate limiter - 20 requests per minute
	inboundLimiter = NewRateLimiter(20, time.Minute)
	
	// Stats rate limiter - 60 requests per minute
	statsLimiter = NewRateLimiter(60, time.Minute)
)

// getClientID extracts client identifier from request
func getClientID(c *gin.Context) string {
	// Use X-Forwarded-For if available (for proxy scenarios)
	clientIP := c.GetHeader("X-Forwarded-For")
	if clientIP == "" {
		clientIP = c.GetHeader("X-Real-IP")
	}
	if clientIP == "" {
		clientIP = c.ClientIP()
	}
	
	// Include user agent for additional uniqueness
	userAgent := c.GetHeader("User-Agent")
	return fmt.Sprintf("%s:%s", clientIP, userAgent)
}

// GeneralRateLimit applies general rate limiting
func GeneralRateLimit() gin.HandlerFunc {
	return rateLimitHandler(generalLimiter, "GENERAL")
}

// LoginRateLimit applies stricter rate limiting for login endpoints
func LoginRateLimit() gin.HandlerFunc {
	return rateLimitHandler(loginLimiter, "LOGIN")
}

// InboundRateLimit applies rate limiting for inbound management
func InboundRateLimit() gin.HandlerFunc {
	return rateLimitHandler(inboundLimiter, "INBOUND")
}

// StatsRateLimit applies rate limiting for stats endpoints
func StatsRateLimit() gin.HandlerFunc {
	return rateLimitHandler(statsLimiter, "STATS")
}

// rateLimitHandler creates a gin handler for rate limiting
func rateLimitHandler(limiter *RateLimiter, limitType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := getClientID(c)
		
		if !limiter.Allow(clientID) {
			// Get client info for headers
			client := limiter.GetClientInfo(clientID)
			
			// Add rate limit headers
			c.Header("X-RateLimit-Limit", strconv.Itoa(limiter.rate))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Type", limitType)
			
			if client != nil && client.blocked {
				retryAfter := int(time.Until(client.blockUntil).Seconds())
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "RATE_LIMIT_EXCEEDED",
					"message": "Too many requests. Please try again later.",
					"type":    limitType,
				},
			})
			c.Abort()
			return
		}
		
		// Add rate limit info to headers
		client := limiter.GetClientInfo(clientID)
		if client != nil {
			c.Header("X-RateLimit-Limit", strconv.Itoa(limiter.rate))
			c.Header("X-RateLimit-Remaining", strconv.Itoa(client.tokens))
			c.Header("X-RateLimit-Type", limitType)
		}
		
		c.Next()
	}
}

// CustomRateLimit creates a custom rate limiter for specific needs
func CustomRateLimit(rate int, period time.Duration, limitType string) gin.HandlerFunc {
	limiter := NewRateLimiter(rate, period)
	return rateLimitHandler(limiter, limitType)
}

// BruteForceProtection provides protection against brute force attacks
func BruteForceProtection() gin.HandlerFunc {
	// Aggressive rate limiter for brute force protection - 3 attempts per 15 minutes
	bruteForceLimiter := NewRateLimiter(3, time.Minute*15)
	
	return func(c *gin.Context) {
		clientID := getClientID(c)
		
		// Only apply to authentication endpoints
		path := c.Request.URL.Path
		authPaths := []string{"/login", "/auth", "/api/login"}
		
		isAuthPath := false
		for _, authPath := range authPaths {
			if path == authPath || (len(path) > len(authPath) && path[:len(authPath)] == authPath) {
				isAuthPath = true
				break
			}
		}
		
		if isAuthPath {
			if !bruteForceLimiter.Allow(clientID) {
				logger.Warning(fmt.Sprintf("Brute force attempt detected from: %s", clientID))
				
				c.JSON(http.StatusTooManyRequests, gin.H{
					"success": false,
					"error": gin.H{
						"code":    "BRUTE_FORCE_DETECTED",
						"message": "Too many failed login attempts. Account temporarily locked.",
					},
				})
				c.Abort()
				return
			}
		}
		
		c.Next()
	}
}

// IPWhitelist middleware for allowing specific IPs
func IPWhitelist(allowedIPs []string) gin.HandlerFunc {
	ipMap := make(map[string]bool)
	for _, ip := range allowedIPs {
		ipMap[ip] = true
	}
	
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		
		// Allow localhost and private IPs by default
		if clientIP == "127.0.0.1" || clientIP == "::1" {
			c.Next()
			return
		}
		
		if len(ipMap) > 0 && !ipMap[clientIP] {
			logger.Warning(fmt.Sprintf("Unauthorized IP access attempt: %s", clientIP))
			
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "IP_NOT_ALLOWED",
					"message": "Your IP address is not authorized to access this resource.",
				},
			})
			c.Abort()
			return
		}
		
		c.Next()
	}
}

// RequestLogger logs rate limit events
func RequestLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format(time.RFC1123),
			param.Method,
			param.Path,
			param.Request.Proto,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	})
}