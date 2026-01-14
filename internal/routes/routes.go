package routes

import (
	"github.com/gin-gonic/gin"

	libauth "github.com/niaga-platform/lib-common/auth"
	libmiddleware "github.com/niaga-platform/lib-common/middleware"
	"github.com/niaga-platform/service-marketplace/internal/handlers"
)

// RouteConfig holds configuration for routes
type RouteConfig struct {
	ConnectionHandler *handlers.ConnectionHandler
	ProductHandler    *handlers.ProductHandler
	CategoryHandler   *handlers.CategoryHandler
	InventoryHandler  *handlers.InventoryHandler
	OrderHandler      *handlers.OrderHandler
	WebhookHandler    *handlers.WebhookHandler
	AnalyticsHandler  *handlers.AnalyticsHandler
	JWTManager        *libauth.JWTManager
}

// SetupRoutes configures all API routes
func SetupRoutes(router *gin.Engine, cfg *RouteConfig) {
	// API v1 routes
	v1 := router.Group("/api/v1")

	// Webhook routes (public - no auth required)
	webhooks := v1.Group("/webhooks")
	{
		if cfg.WebhookHandler != nil {
			webhooks.POST("/shopee", cfg.WebhookHandler.HandleShopeeWebhook)
			webhooks.POST("/tiktok", cfg.WebhookHandler.HandleTikTokWebhook)
		} else {
			webhooks.POST("/shopee", handleShopeeWebhookPlaceholder)
			webhooks.POST("/tiktok", handleTikTokWebhookPlaceholder)
		}
	}

	// Admin marketplace routes (require authentication and admin role)
	admin := v1.Group("/admin/marketplace")
	admin.Use(libmiddleware.AuthMiddleware(cfg.JWTManager))
	admin.Use(libmiddleware.RequireAdmin())
	{
		// Connection management
		connections := admin.Group("/connections")
		{
			connections.GET("", cfg.ConnectionHandler.GetConnections)
			connections.GET("/active", cfg.ConnectionHandler.GetActiveConnections)
			connections.GET("/:id", cfg.ConnectionHandler.GetConnection)
			connections.DELETE("/:id", cfg.ConnectionHandler.Disconnect)
			connections.POST("/:id/refresh", cfg.ConnectionHandler.RefreshToken)

			// Product sync routes
			connections.GET("/:id/products", cfg.ProductHandler.GetMappedProducts)
			connections.POST("/:id/products/push", cfg.ProductHandler.PushProducts)
			connections.PUT("/:id/products/:mapping_id", cfg.ProductHandler.UpdateProductMapping)
			connections.DELETE("/:id/products/:mapping_id", cfg.ProductHandler.DeleteProductMapping)

			// Import & Map routes (for linking existing marketplace products to admin products)
			connections.POST("/:id/products/import", cfg.ProductHandler.ImportProducts)
			connections.GET("/:id/products/imported", cfg.ProductHandler.GetImportedProducts)
			connections.POST("/:id/products/map", cfg.ProductHandler.CreateManualMapping)
			connections.DELETE("/:id/products/map/:mapping_id", cfg.ProductHandler.DeleteManualMapping)

			// Category mapping routes
			connections.GET("/:id/categories/external", cfg.CategoryHandler.GetExternalCategories)
			connections.GET("/:id/categories", cfg.CategoryHandler.GetCategoryMappings)
			connections.POST("/:id/categories", cfg.CategoryHandler.CreateCategoryMapping)
			connections.DELETE("/:id/categories/:mapping_id", cfg.CategoryHandler.DeleteCategoryMapping)

			// Inventory sync routes
			connections.POST("/:id/inventory/push", cfg.InventoryHandler.PushInventory)
			connections.POST("/:id/inventory/status", cfg.InventoryHandler.GetInventoryStatus)

			// Order sync routes
			connections.GET("/:id/orders", cfg.OrderHandler.GetOrders)
			connections.POST("/:id/orders/sync", cfg.OrderHandler.SyncOrders)
			connections.PUT("/:id/orders/:order_id/status", cfg.OrderHandler.UpdateOrderStatus)
			connections.POST("/:id/orders/:order_id/ship", cfg.OrderHandler.ArrangeShipment)
			connections.POST("/:id/orders/:order_id/awb", cfg.OrderHandler.GetAWB)

			// Analytics routes
			if cfg.AnalyticsHandler != nil {
				connections.GET("/:id/analytics", cfg.AnalyticsHandler.GetAnalytics)
				connections.GET("/:id/analytics/performance", cfg.AnalyticsHandler.GetPerformance)
				connections.GET("/:id/analytics/daily-sales", cfg.AnalyticsHandler.GetDailySales)
				connections.GET("/:id/analytics/top-products", cfg.AnalyticsHandler.GetTopProducts)
			}
		}

		// OAuth flow
		admin.POST("/:platform/auth-url", cfg.ConnectionHandler.GetAuthURL)
		admin.GET("/shopee/callback", cfg.ConnectionHandler.HandleShopeeCallback)
		admin.GET("/tiktok/callback", cfg.ConnectionHandler.HandleTikTokCallback)
	}
}

// handleShopeeWebhookPlaceholder handles incoming Shopee webhooks (placeholder)
func handleShopeeWebhookPlaceholder(c *gin.Context) {
	c.JSON(200, gin.H{"status": "received"})
}

// handleTikTokWebhookPlaceholder handles incoming TikTok webhooks (placeholder)
func handleTikTokWebhookPlaceholder(c *gin.Context) {
	c.JSON(200, gin.H{"status": "received"})
}
