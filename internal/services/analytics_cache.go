package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

// AnalyticsCacheService handles caching for marketplace analytics
type AnalyticsCacheService struct {
	redis  *redis.Client
	ttl    time.Duration
	logger *zap.Logger
}

// CachedAnalytics represents the cached analytics data
type CachedAnalytics struct {
	Performance    *providers.ShopPerformance  `json:"performance,omitempty"`
	DailySales     []providers.DailySales      `json:"daily_sales,omitempty"`
	TopProducts    []providers.TopProduct      `json:"top_products,omitempty"`
	TrafficSources []providers.TrafficSource   `json:"traffic_sources,omitempty"`
	CachedAt       time.Time                   `json:"cached_at"`
}

// NewAnalyticsCacheService creates a new analytics cache service
func NewAnalyticsCacheService(redisClient *redis.Client, ttl time.Duration, logger *zap.Logger) *AnalyticsCacheService {
	if ttl == 0 {
		ttl = 10 * time.Minute // Default TTL
	}
	return &AnalyticsCacheService{
		redis:  redisClient,
		ttl:    ttl,
		logger: logger,
	}
}

// cacheKey generates a cache key for analytics data
func (s *AnalyticsCacheService) cacheKey(connectionID, startDate, endDate string) string {
	return fmt.Sprintf("marketplace:analytics:%s:%s:%s", connectionID, startDate, endDate)
}

// Get retrieves cached analytics data
func (s *AnalyticsCacheService) Get(ctx context.Context, connectionID, startDate, endDate string) (*CachedAnalytics, error) {
	if s.redis == nil {
		return nil, nil // No cache available
	}

	key := s.cacheKey(connectionID, startDate, endDate)
	data, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Cache miss
		}
		s.logger.Warn("failed to get analytics from cache", zap.Error(err), zap.String("key", key))
		return nil, nil
	}

	var cached CachedAnalytics
	if err := json.Unmarshal(data, &cached); err != nil {
		s.logger.Warn("failed to unmarshal cached analytics", zap.Error(err))
		return nil, nil
	}

	s.logger.Debug("cache hit for analytics", zap.String("connection_id", connectionID))
	return &cached, nil
}

// Set stores analytics data in cache
func (s *AnalyticsCacheService) Set(ctx context.Context, connectionID, startDate, endDate string, analytics *CachedAnalytics) error {
	if s.redis == nil {
		return nil // No cache available
	}

	analytics.CachedAt = time.Now()
	key := s.cacheKey(connectionID, startDate, endDate)

	data, err := json.Marshal(analytics)
	if err != nil {
		s.logger.Warn("failed to marshal analytics for cache", zap.Error(err))
		return err
	}

	if err := s.redis.Set(ctx, key, data, s.ttl).Err(); err != nil {
		s.logger.Warn("failed to set analytics in cache", zap.Error(err), zap.String("key", key))
		return err
	}

	s.logger.Debug("cached analytics", zap.String("connection_id", connectionID), zap.Duration("ttl", s.ttl))
	return nil
}

// Invalidate removes cached analytics for a connection
func (s *AnalyticsCacheService) Invalidate(ctx context.Context, connectionID string) error {
	if s.redis == nil {
		return nil
	}

	pattern := fmt.Sprintf("marketplace:analytics:%s:*", connectionID)
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		s.logger.Warn("failed to find cache keys to invalidate", zap.Error(err))
		return err
	}

	if len(keys) > 0 {
		if err := s.redis.Del(ctx, keys...).Err(); err != nil {
			s.logger.Warn("failed to invalidate analytics cache", zap.Error(err))
			return err
		}
		s.logger.Debug("invalidated analytics cache", zap.String("connection_id", connectionID), zap.Int("keys_removed", len(keys)))
	}

	return nil
}

// GetPerformance retrieves cached performance data
func (s *AnalyticsCacheService) GetPerformance(ctx context.Context, connectionID, startDate, endDate string) (*providers.ShopPerformance, error) {
	cached, err := s.Get(ctx, connectionID, startDate, endDate)
	if err != nil || cached == nil {
		return nil, err
	}
	return cached.Performance, nil
}

// GetDailySales retrieves cached daily sales data
func (s *AnalyticsCacheService) GetDailySales(ctx context.Context, connectionID, startDate, endDate string) ([]providers.DailySales, error) {
	cached, err := s.Get(ctx, connectionID, startDate, endDate)
	if err != nil || cached == nil {
		return nil, err
	}
	return cached.DailySales, nil
}

// GetTopProducts retrieves cached top products data
func (s *AnalyticsCacheService) GetTopProducts(ctx context.Context, connectionID, startDate, endDate string) ([]providers.TopProduct, error) {
	cached, err := s.Get(ctx, connectionID, startDate, endDate)
	if err != nil || cached == nil {
		return nil, err
	}
	return cached.TopProducts, nil
}
