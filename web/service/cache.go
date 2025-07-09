package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"x-ui/database/model"
	"x-ui/logger"
)

// CacheService handles all caching operations
type CacheService struct {
	client *redis.Client
	ctx    context.Context
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// Cache key prefixes
const (
	InboundStatsPrefix    = "inbound_stats:"
	InboundConfigPrefix   = "inbound_config:"
	UserStatsPrefix       = "user_stats:"
	SystemStatsPrefix     = "system_stats:"
	TrafficStatsPrefix    = "traffic_stats:"
	ClientIPPrefix        = "client_ip:"
	SettingsPrefix        = "settings:"
	OnlineUsersPrefix     = "online_users:"
)

// Cache TTL durations
const (
	ShortTTL  = 5 * time.Minute
	MediumTTL = 30 * time.Minute
	LongTTL   = 2 * time.Hour
	DayTTL    = 24 * time.Hour
)

var cacheService *CacheService

// NewCacheService creates a new cache service
func NewCacheService(config *CacheConfig) *CacheService {
	if config == nil {
		// Default configuration
		config = &CacheConfig{
			Host:     "localhost",
			Port:     6379,
			Password: "",
			DB:       0,
			PoolSize: 10,
		}
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.DB,
		PoolSize: config.PoolSize,
	})

	ctx := context.Background()

	// Test connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		logger.Warning("Redis connection failed, cache will be disabled:", err)
		return &CacheService{client: nil, ctx: ctx}
	}

	logger.Info("Redis cache service initialized successfully")

	cacheService = &CacheService{
		client: rdb,
		ctx:    ctx,
	}

	return cacheService
}

// GetCacheService returns the global cache service instance
func GetCacheService() *CacheService {
	if cacheService == nil {
		return NewCacheService(nil)
	}
	return cacheService
}

// IsEnabled checks if cache is enabled
func (c *CacheService) IsEnabled() bool {
	return c.client != nil
}

// Set stores a value in cache
func (c *CacheService) Set(key string, value interface{}, ttl time.Duration) error {
	if !c.IsEnabled() {
		return nil
	}

	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return c.client.Set(c.ctx, key, jsonData, ttl).Err()
}

// Get retrieves a value from cache
func (c *CacheService) Get(key string, dest interface{}) error {
	if !c.IsEnabled() {
		return redis.Nil
	}

	val, err := c.client.Get(c.ctx, key).Result()
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(val), dest)
}

// Delete removes a key from cache
func (c *CacheService) Delete(key string) error {
	if !c.IsEnabled() {
		return nil
	}

	return c.client.Del(c.ctx, key).Err()
}

// DeletePattern removes all keys matching pattern
func (c *CacheService) DeletePattern(pattern string) error {
	if !c.IsEnabled() {
		return nil
	}

	keys, err := c.client.Keys(c.ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		return c.client.Del(c.ctx, keys...).Err()
	}

	return nil
}

// Exists checks if key exists
func (c *CacheService) Exists(key string) bool {
	if !c.IsEnabled() {
		return false
	}

	result, err := c.client.Exists(c.ctx, key).Result()
	return err == nil && result > 0
}

// SetTTL sets TTL for existing key
func (c *CacheService) SetTTL(key string, ttl time.Duration) error {
	if !c.IsEnabled() {
		return nil
	}

	return c.client.Expire(c.ctx, key, ttl).Err()
}

// Inbound cache methods

// SetInboundStats caches inbound statistics
func (c *CacheService) SetInboundStats(id int, stats *model.Inbound) error {
	key := InboundStatsPrefix + strconv.Itoa(id)
	return c.Set(key, stats, ShortTTL)
}

// GetInboundStats retrieves cached inbound statistics
func (c *CacheService) GetInboundStats(id int) (*model.Inbound, error) {
	key := InboundStatsPrefix + strconv.Itoa(id)
	var stats model.Inbound
	err := c.Get(key, &stats)
	if err == redis.Nil {
		return nil, nil
	}
	return &stats, err
}

// SetInboundConfig caches inbound configuration
func (c *CacheService) SetInboundConfig(id int, config *model.Inbound) error {
	key := InboundConfigPrefix + strconv.Itoa(id)
	return c.Set(key, config, MediumTTL)
}

// GetInboundConfig retrieves cached inbound configuration
func (c *CacheService) GetInboundConfig(id int) (*model.Inbound, error) {
	key := InboundConfigPrefix + strconv.Itoa(id)
	var config model.Inbound
	err := c.Get(key, &config)
	if err == redis.Nil {
		return nil, nil
	}
	return &config, err
}

// InvalidateInbound removes all cached data for an inbound
func (c *CacheService) InvalidateInbound(id int) error {
	patterns := []string{
		InboundStatsPrefix + strconv.Itoa(id),
		InboundConfigPrefix + strconv.Itoa(id),
		TrafficStatsPrefix + strconv.Itoa(id) + ":*",
	}

	for _, pattern := range patterns {
		if err := c.DeletePattern(pattern); err != nil {
			logger.Warning("Failed to invalidate cache pattern:", pattern, err)
		}
	}

	return nil
}

// User and Client cache methods

// SetUserStats caches user statistics
func (c *CacheService) SetUserStats(userID int, stats interface{}) error {
	key := UserStatsPrefix + strconv.Itoa(userID)
	return c.Set(key, stats, ShortTTL)
}

// GetUserStats retrieves cached user statistics
func (c *CacheService) GetUserStats(userID int, dest interface{}) error {
	key := UserStatsPrefix + strconv.Itoa(userID)
	return c.Get(key, dest)
}

// SetClientIP caches client IP information
func (c *CacheService) SetClientIP(email string, ips []string) error {
	key := ClientIPPrefix + email
	return c.Set(key, ips, MediumTTL)
}

// GetClientIP retrieves cached client IP information
func (c *CacheService) GetClientIP(email string) ([]string, error) {
	key := ClientIPPrefix + email
	var ips []string
	err := c.Get(key, &ips)
	if err == redis.Nil {
		return nil, nil
	}
	return ips, err
}

// System cache methods

// SetSystemStats caches system statistics
func (c *CacheService) SetSystemStats(stats interface{}) error {
	key := SystemStatsPrefix + "current"
	return c.Set(key, stats, ShortTTL)
}

// GetSystemStats retrieves cached system statistics
func (c *CacheService) GetSystemStats(dest interface{}) error {
	key := SystemStatsPrefix + "current"
	return c.Get(key, dest)
}

// Settings cache methods

// SetSetting caches a setting
func (c *CacheService) SetSetting(key string, value interface{}) error {
	cacheKey := SettingsPrefix + key
	return c.Set(cacheKey, value, LongTTL)
}

// GetSetting retrieves a cached setting
func (c *CacheService) GetSetting(key string, dest interface{}) error {
	cacheKey := SettingsPrefix + key
	return c.Get(cacheKey, dest)
}

// InvalidateSettings removes all cached settings
func (c *CacheService) InvalidateSettings() error {
	return c.DeletePattern(SettingsPrefix + "*")
}

// Traffic statistics cache methods

// SetTrafficStats caches traffic statistics
func (c *CacheService) SetTrafficStats(inboundID int, period string, stats interface{}) error {
	key := fmt.Sprintf("%s%d:%s", TrafficStatsPrefix, inboundID, period)
	return c.Set(key, stats, MediumTTL)
}

// GetTrafficStats retrieves cached traffic statistics
func (c *CacheService) GetTrafficStats(inboundID int, period string, dest interface{}) error {
	key := fmt.Sprintf("%s%d:%s", TrafficStatsPrefix, inboundID, period)
	return c.Get(key, dest)
}

// Online users tracking

// SetOnlineUser marks a user as online
func (c *CacheService) SetOnlineUser(userID int, sessionInfo interface{}) error {
	key := OnlineUsersPrefix + strconv.Itoa(userID)
	return c.Set(key, sessionInfo, 10*time.Minute) // 10 minutes online timeout
}

// GetOnlineUsers retrieves all online users
func (c *CacheService) GetOnlineUsers() ([]int, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	keys, err := c.client.Keys(c.ctx, OnlineUsersPrefix+"*").Result()
	if err != nil {
		return nil, err
	}

	var userIDs []int
	for _, key := range keys {
		userIDStr := key[len(OnlineUsersPrefix):]
		if userID, err := strconv.Atoi(userIDStr); err == nil {
			userIDs = append(userIDs, userID)
		}
	}

	return userIDs, nil
}

// RemoveOnlineUser removes user from online list
func (c *CacheService) RemoveOnlineUser(userID int) error {
	key := OnlineUsersPrefix + strconv.Itoa(userID)
	return c.Delete(key)
}

// Batch operations

// SetMultiple stores multiple key-value pairs
func (c *CacheService) SetMultiple(data map[string]interface{}, ttl time.Duration) error {
	if !c.IsEnabled() {
		return nil
	}

	pipe := c.client.Pipeline()
	
	for key, value := range data {
		jsonData, err := json.Marshal(value)
		if err != nil {
			continue
		}
		pipe.Set(c.ctx, key, jsonData, ttl)
	}

	_, err := pipe.Exec(c.ctx)
	return err
}

// GetMultiple retrieves multiple keys
func (c *CacheService) GetMultiple(keys []string) (map[string]interface{}, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	pipe := c.client.Pipeline()
	
	for _, key := range keys {
		pipe.Get(c.ctx, key)
	}

	results, err := pipe.Exec(c.ctx)
	if err != nil {
		return nil, err
	}

	data := make(map[string]interface{})
	for i, result := range results {
		if cmd, ok := result.(*redis.StringCmd); ok {
			val, err := cmd.Result()
			if err == nil {
				var value interface{}
				if json.Unmarshal([]byte(val), &value) == nil {
					data[keys[i]] = value
				}
			}
		}
	}

	return data, nil
}

// Cache statistics and monitoring

// GetCacheStats returns cache statistics
func (c *CacheService) GetCacheStats() (map[string]interface{}, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	info, err := c.client.Info(c.ctx, "memory", "stats").Result()
	if err != nil {
		return nil, err
	}

	dbSize, err := c.client.DBSize(c.ctx).Result()
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"db_size":    dbSize,
		"redis_info": info,
		"enabled":    true,
	}

	return stats, nil
}

// FlushAll clears all cache
func (c *CacheService) FlushAll() error {
	if !c.IsEnabled() {
		return nil
	}

	logger.Warning("Flushing all cache data")
	return c.client.FlushAll(c.ctx).Err()
}

// Close closes the Redis connection
func (c *CacheService) Close() error {
	if !c.IsEnabled() {
		return nil
	}

	return c.client.Close()
}

// Utility methods

// GenerateKey creates a standardized cache key
func (c *CacheService) GenerateKey(prefix, id string, suffix ...string) string {
	key := prefix + id
	for _, s := range suffix {
		key += ":" + s
	}
	return key
}

// GetKeysByPattern retrieves all keys matching a pattern
func (c *CacheService) GetKeysByPattern(pattern string) ([]string, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	return c.client.Keys(c.ctx, pattern).Result()
}

// SetWithLock sets a value with distributed lock
func (c *CacheService) SetWithLock(key string, value interface{}, ttl time.Duration, lockTTL time.Duration) error {
	if !c.IsEnabled() {
		return nil
	}

	lockKey := "lock:" + key
	
	// Try to acquire lock
	locked, err := c.client.SetNX(c.ctx, lockKey, "locked", lockTTL).Result()
	if err != nil {
		return err
	}
	
	if !locked {
		return fmt.Errorf("could not acquire lock for key: %s", key)
	}
	
	// Ensure lock is released
	defer c.client.Del(c.ctx, lockKey)
	
	return c.Set(key, value, ttl)
}