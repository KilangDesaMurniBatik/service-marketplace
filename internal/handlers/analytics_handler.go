package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/services"
)

// AnalyticsHandler handles marketplace analytics endpoints
type AnalyticsHandler struct {
	connService     *services.ConnectionService
	providerFactory *services.ProviderFactoryService
	cacheService    *services.AnalyticsCacheService
	logger          *zap.Logger
}

// NewAnalyticsHandler creates a new analytics handler
func NewAnalyticsHandler(
	connService *services.ConnectionService,
	providerFactory *services.ProviderFactoryService,
	cacheService *services.AnalyticsCacheService,
	logger *zap.Logger,
) *AnalyticsHandler {
	return &AnalyticsHandler{
		connService:     connService,
		providerFactory: providerFactory,
		cacheService:    cacheService,
		logger:          logger,
	}
}

// GetAnalytics returns combined analytics for a marketplace connection
// @Summary Get marketplace analytics
// @Tags Analytics
// @Param id path string true "Connection ID"
// @Param start_date query string false "Start date (YYYY-MM-DD)"
// @Param end_date query string false "End date (YYYY-MM-DD)"
// @Param refresh query bool false "Force refresh (bypass cache)"
// @Success 200 {object} providers.AnalyticsResponse
// @Router /admin/marketplace/connections/{id}/analytics [get]
func (h *AnalyticsHandler) GetAnalytics(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	// Parse date range
	startDateStr := c.DefaultQuery("start_date", "")
	endDateStr := c.DefaultQuery("end_date", "")
	forceRefresh := c.Query("refresh") == "true"

	var startDate, endDate time.Time
	if startDateStr == "" {
		startDate = time.Now().AddDate(0, 0, -30)
		startDateStr = startDate.Format("2006-01-02")
	} else {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format"})
			return
		}
	}

	if endDateStr == "" {
		endDate = time.Now()
		endDateStr = endDate.Format("2006-01-02")
	} else {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format"})
			return
		}
	}

	// Get connection
	conn, err := h.connService.GetConnection(c.Request.Context(), connectionID)
	if err != nil {
		h.logger.Error("failed to get connection", zap.Error(err), zap.String("connection_id", connectionID.String()))
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	// Try to get from cache first (unless force refresh)
	var cached *services.CachedAnalytics
	var fromCache bool
	if h.cacheService != nil && !forceRefresh {
		cached, _ = h.cacheService.Get(c.Request.Context(), connectionID.String(), startDateStr, endDateStr)
		if cached != nil {
			fromCache = true
			h.logger.Debug("serving analytics from cache", zap.String("connection_id", connectionID.String()))
		}
	}

	var response providers.AnalyticsResponse

	if cached != nil {
		// Use cached data
		response.Performance = cached.Performance
		response.DailySales = cached.DailySales
		response.TopProducts = cached.TopProducts
		response.TrafficSource = cached.TrafficSources
	} else {
		// Fetch fresh data from provider
		provider, err := h.providerFactory.CreateShopeeProviderForConnection(c.Request.Context(), connectionID)
		if err != nil {
			h.logger.Error("failed to get provider", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize provider"})
			return
		}

		// Check if provider supports analytics
		analyticsProvider, ok := interface{}(provider).(AnalyticsProvider)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "This marketplace does not support analytics"})
			return
		}

		params := providers.AnalyticsQueryParams{
			StartDate: startDate,
			EndDate:   endDate,
			Limit:     10,
		}

		// Get all analytics data
		performance, err := analyticsProvider.GetShopPerformance(c.Request.Context(), params)
		if err != nil {
			h.logger.Warn("failed to get shop performance", zap.Error(err))
		}
		response.Performance = performance

		dailySales, err := analyticsProvider.GetDailySales(c.Request.Context(), params)
		if err != nil {
			h.logger.Warn("failed to get daily sales", zap.Error(err))
		}
		response.DailySales = dailySales

		topProducts, err := analyticsProvider.GetTopProducts(c.Request.Context(), params)
		if err != nil {
			h.logger.Warn("failed to get top products", zap.Error(err))
		}
		response.TopProducts = topProducts

		trafficSources, err := analyticsProvider.GetTrafficSources(c.Request.Context(), params)
		if err != nil {
			h.logger.Warn("failed to get traffic sources", zap.Error(err))
		}
		response.TrafficSource = trafficSources

		// Store in cache
		if h.cacheService != nil {
			cacheData := &services.CachedAnalytics{
				Performance:    response.Performance,
				DailySales:     response.DailySales,
				TopProducts:    response.TopProducts,
				TrafficSources: response.TrafficSource,
			}
			if err := h.cacheService.Set(c.Request.Context(), connectionID.String(), startDateStr, endDateStr, cacheData); err != nil {
				h.logger.Warn("failed to cache analytics", zap.Error(err))
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"connection_id": connectionID.String(),
		"platform":      conn.Platform,
		"shop_name":     conn.ShopName,
		"date_range": gin.H{
			"start": startDateStr,
			"end":   endDateStr,
		},
		"analytics":  response,
		"from_cache": fromCache,
	})
}

// GetPerformance returns shop performance metrics
// @Summary Get shop performance
// @Tags Analytics
// @Param id path string true "Connection ID"
// @Success 200 {object} providers.ShopPerformance
// @Router /admin/marketplace/connections/{id}/analytics/performance [get]
func (h *AnalyticsHandler) GetPerformance(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	startDateStr := c.DefaultQuery("start_date", time.Now().AddDate(0, 0, -30).Format("2006-01-02"))
	endDateStr := c.DefaultQuery("end_date", time.Now().Format("2006-01-02"))

	startDate, _ := time.Parse("2006-01-02", startDateStr)
	endDate, _ := time.Parse("2006-01-02", endDateStr)

	_, err = h.connService.GetConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	provider, err := h.providerFactory.CreateShopeeProviderForConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize provider"})
		return
	}

	analyticsProvider, ok := interface{}(provider).(AnalyticsProvider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Analytics not supported"})
		return
	}

	params := providers.AnalyticsQueryParams{
		StartDate: startDate,
		EndDate:   endDate,
	}

	performance, err := analyticsProvider.GetShopPerformance(c.Request.Context(), params)
	if err != nil {
		h.logger.Error("failed to get performance", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get performance"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"performance": performance})
}

// GetDailySales returns daily sales data
// @Summary Get daily sales
// @Tags Analytics
// @Param id path string true "Connection ID"
// @Success 200 {array} providers.DailySales
// @Router /admin/marketplace/connections/{id}/analytics/daily-sales [get]
func (h *AnalyticsHandler) GetDailySales(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	startDateStr := c.DefaultQuery("start_date", time.Now().AddDate(0, 0, -30).Format("2006-01-02"))
	endDateStr := c.DefaultQuery("end_date", time.Now().Format("2006-01-02"))

	startDate, _ := time.Parse("2006-01-02", startDateStr)
	endDate, _ := time.Parse("2006-01-02", endDateStr)

	_, err = h.connService.GetConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	provider, err := h.providerFactory.CreateShopeeProviderForConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize provider"})
		return
	}

	analyticsProvider, ok := interface{}(provider).(AnalyticsProvider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Analytics not supported"})
		return
	}

	params := providers.AnalyticsQueryParams{
		StartDate: startDate,
		EndDate:   endDate,
	}

	sales, err := analyticsProvider.GetDailySales(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get daily sales"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"daily_sales": sales})
}

// GetTopProducts returns top-selling products
// @Summary Get top products
// @Tags Analytics
// @Param id path string true "Connection ID"
// @Success 200 {array} providers.TopProduct
// @Router /admin/marketplace/connections/{id}/analytics/top-products [get]
func (h *AnalyticsHandler) GetTopProducts(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	startDateStr := c.DefaultQuery("start_date", time.Now().AddDate(0, 0, -30).Format("2006-01-02"))
	endDateStr := c.DefaultQuery("end_date", time.Now().Format("2006-01-02"))

	startDate, _ := time.Parse("2006-01-02", startDateStr)
	endDate, _ := time.Parse("2006-01-02", endDateStr)

	_, err = h.connService.GetConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	provider, err := h.providerFactory.CreateShopeeProviderForConnection(c.Request.Context(), connectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize provider"})
		return
	}

	analyticsProvider, ok := interface{}(provider).(AnalyticsProvider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Analytics not supported"})
		return
	}

	params := providers.AnalyticsQueryParams{
		StartDate: startDate,
		EndDate:   endDate,
		Limit:     10,
	}

	products, err := analyticsProvider.GetTopProducts(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get top products"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"top_products": products})
}

// AnalyticsProvider interface for providers that support analytics
type AnalyticsProvider interface {
	GetShopPerformance(ctx context.Context, params providers.AnalyticsQueryParams) (*providers.ShopPerformance, error)
	GetDailySales(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.DailySales, error)
	GetTopProducts(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.TopProduct, error)
	GetTrafficSources(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.TrafficSource, error)
}
