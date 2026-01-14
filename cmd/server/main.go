package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/clients"
	"github.com/niaga-platform/service-marketplace/internal/config"
	"github.com/niaga-platform/service-marketplace/internal/events"
	"github.com/niaga-platform/service-marketplace/internal/handlers"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/routes"
	"github.com/niaga-platform/service-marketplace/internal/services"

	"github.com/nats-io/nats.go"
	libauth "github.com/niaga-platform/lib-common/auth"
	libdb "github.com/niaga-platform/lib-common/database"
	liblogger "github.com/niaga-platform/lib-common/logger"
	libmiddleware "github.com/niaga-platform/lib-common/middleware"
	"github.com/niaga-platform/lib-common/monitoring"
)

func main() {
	// Load .env file in development
	if os.Getenv("APP_ENV") != "production" {
		_ = godotenv.Load()
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger, err := liblogger.NewLogger(cfg.App.Env)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Initialize Sentry for error tracking
	sentryMonitor, err := monitoring.NewSentryMonitor(&monitoring.SentryConfig{
		DSN:              cfg.Sentry.DSN,
		Environment:      cfg.Sentry.Environment,
		Release:          cfg.Sentry.Release,
		ServiceName:      "marketplace-service",
		TracesSampleRate: 0.1,
	}, logger)
	if err != nil {
		logger.Warn("Failed to initialize Sentry", zap.Error(err))
	}
	defer sentryMonitor.Flush(2 * time.Second)

	// Connect to database
	dbConfig := libdb.PostgresConfig{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		Database: cfg.Database.Database,
		SSLMode:  cfg.Database.SSLMode,
	}

	db, err := libdb.Connect(dbConfig, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// Initialize JWT manager for auth middleware
	jwtManager := libauth.NewJWTManager(
		cfg.JWT.Secret,
		15*time.Minute,
		168*time.Hour,
	)

	// Initialize repositories
	connectionRepo := repository.NewConnectionRepository(db)
	productMappingRepo := repository.NewProductMappingRepository(db)
	categoryMappingRepo := repository.NewCategoryMappingRepository(db)
	syncJobRepo := repository.NewSyncJobRepository(db)
	orderRepo := repository.NewMarketplaceOrderRepository(db)
	importedProductRepo := repository.NewImportedProductRepository(db)

	// Initialize catalog client
	catalogClient := clients.NewCatalogClient(cfg.Services.CatalogURL, logger)

	// Log repository initialization
	logger.Info("Repositories initialized",
		zap.Bool("connectionRepo", connectionRepo != nil),
		zap.Bool("productMappingRepo", productMappingRepo != nil),
		zap.Bool("categoryMappingRepo", categoryMappingRepo != nil),
		zap.Bool("syncJobRepo", syncJobRepo != nil),
		zap.Bool("orderRepo", orderRepo != nil),
	)

	// Initialize connection service
	connectionService, err := services.NewConnectionService(
		connectionRepo,
		&services.ConnectionServiceConfig{
			EncryptionKey:     cfg.Security.EncryptionKey,
			ShopeePartnerID:   cfg.Shopee.PartnerID,
			ShopeePartnerKey:  cfg.Shopee.PartnerKey,
			ShopeeRedirectURL: cfg.Shopee.RedirectURL,
			ShopeeSandbox:     cfg.Shopee.IsSandbox,
			TikTokAppKey:      cfg.TikTok.AppKey,
			TikTokAppSecret:   cfg.TikTok.AppSecret,
			TikTokRedirectURL: cfg.TikTok.RedirectURL,
		},
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to initialize connection service", zap.Error(err))
	}

	// Initialize product sync service
	productSyncService, err := services.NewProductSyncService(
		connectionRepo,
		productMappingRepo,
		categoryMappingRepo,
		syncJobRepo,
		importedProductRepo,
		catalogClient,
		&services.ProductSyncServiceConfig{
			ShopeePartnerID:  cfg.Shopee.PartnerID,
			ShopeePartnerKey: cfg.Shopee.PartnerKey,
			ShopeeSandbox:    cfg.Shopee.IsSandbox,
			TikTokAppKey:     cfg.TikTok.AppKey,
			TikTokAppSecret:  cfg.TikTok.AppSecret,
			EncryptionKey:    cfg.Security.EncryptionKey,
		},
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to initialize product sync service", zap.Error(err))
	}

	// Initialize provider factory service for analytics
	providerFactoryService, err := services.NewProviderFactoryService(
		connectionRepo,
		&services.ProviderFactoryConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
			Shopee: &services.ShopeeProviderConfig{
				PartnerID:   cfg.Shopee.PartnerID,
				PartnerKey:  cfg.Shopee.PartnerKey,
				RedirectURL: cfg.Shopee.RedirectURL,
				IsSandbox:   cfg.Shopee.IsSandbox,
			},
			TikTok: &services.TikTokProviderConfig{
				AppKey:      cfg.TikTok.AppKey,
				AppSecret:   cfg.TikTok.AppSecret,
				RedirectURL: cfg.TikTok.RedirectURL,
			},
		},
		logger,
	)
	if err != nil {
		logger.Warn("Failed to initialize provider factory service, analytics disabled", zap.Error(err))
	}

	// Initialize handlers
	connectionHandler := handlers.NewConnectionHandler(connectionService, logger)
	productHandler := handlers.NewProductHandler(productSyncService, logger)
	categoryHandler := handlers.NewCategoryHandler(productSyncService, logger)

	// Initialize analytics handler (optional)
	var analyticsHandler *handlers.AnalyticsHandler
	if providerFactoryService != nil {
		analyticsHandler = handlers.NewAnalyticsHandler(connectionService, providerFactoryService, logger)
	}

	// Connect to NATS (optional - only if configured)
	var natsConn *nats.Conn
	var eventPublisher *events.Publisher
	var eventSubscriber *events.Subscriber

	if cfg.NATS.URL != "" {
		natsConn, err = nats.Connect(cfg.NATS.URL)
		if err != nil {
			logger.Warn("Failed to connect to NATS, inventory sync disabled", zap.Error(err))
		} else {
			logger.Info("Connected to NATS", zap.String("url", cfg.NATS.URL))
			eventPublisher = events.NewPublisher(natsConn, logger)
		}
	}

	// Initialize inventory sync service
	inventorySyncService, err := services.NewInventorySyncService(
		connectionRepo,
		productMappingRepo,
		eventPublisher,
		&services.InventorySyncServiceConfig{
			ShopeePartnerID:  cfg.Shopee.PartnerID,
			ShopeePartnerKey: cfg.Shopee.PartnerKey,
			ShopeeSandbox:    cfg.Shopee.IsSandbox,
			TikTokAppKey:     cfg.TikTok.AppKey,
			TikTokAppSecret:  cfg.TikTok.AppSecret,
			EncryptionKey:    cfg.Security.EncryptionKey,
		},
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to initialize inventory sync service", zap.Error(err))
	}

	// Initialize marketplace sync handler for auto-sync when products are updated
	marketplaceSyncHandler, err := services.NewMarketplaceSyncHandler(
		connectionRepo,
		productMappingRepo,
		categoryMappingRepo,
		catalogClient,
		eventPublisher,
		&services.MarketplaceSyncHandlerConfig{
			ShopeePartnerID:  cfg.Shopee.PartnerID,
			ShopeePartnerKey: cfg.Shopee.PartnerKey,
			ShopeeSandbox:    cfg.Shopee.IsSandbox,
			EncryptionKey:    cfg.Security.EncryptionKey,
			AutoSyncEnabled:  true, // Enable auto-sync by default
		},
		logger,
	)
	if err != nil {
		logger.Warn("Failed to initialize marketplace sync handler", zap.Error(err))
	}

	// Start NATS subscriber if connected
	if natsConn != nil && marketplaceSyncHandler != nil {
		eventSubscriber = events.NewSubscriber(natsConn, marketplaceSyncHandler, logger)
		if err := eventSubscriber.Start(); err != nil {
			logger.Warn("Failed to start event subscriber", zap.Error(err))
		}
		logger.Info("Auto-sync enabled: Products will sync to marketplaces when updated in admin")
	} else if natsConn != nil {
		// Fallback to inventory sync service if marketplace sync handler failed
		eventSubscriber = events.NewSubscriber(natsConn, inventorySyncService, logger)
		if err := eventSubscriber.Start(); err != nil {
			logger.Warn("Failed to start event subscriber", zap.Error(err))
		}
	}

	// Initialize inventory handler
	inventoryHandler := handlers.NewInventoryHandler(inventorySyncService, logger)

	// Initialize order client
	orderClient := clients.NewOrderClient(cfg.Services.OrderURL, logger)

	// Initialize order sync service
	orderSyncService, err := services.NewOrderSyncService(
		connectionRepo,
		orderRepo,
		orderClient,
		&services.OrderSyncServiceConfig{
			ShopeePartnerID:  cfg.Shopee.PartnerID,
			ShopeePartnerKey: cfg.Shopee.PartnerKey,
			ShopeeSandbox:    cfg.Shopee.IsSandbox,
			TikTokAppKey:     cfg.TikTok.AppKey,
			TikTokAppSecret:  cfg.TikTok.AppSecret,
			EncryptionKey:    cfg.Security.EncryptionKey,
		},
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to initialize order sync service", zap.Error(err))
	}

	// Initialize order handler
	orderHandler := handlers.NewOrderHandler(orderSyncService, logger)

	// Initialize webhook handler
	webhookHandler := handlers.NewWebhookHandler(orderSyncService, &handlers.WebhookConfig{
		ShopeePartnerKey: cfg.Shopee.PartnerKey,
		TikTokAppSecret:  cfg.TikTok.AppSecret,
	}, logger)

	// Set Gin mode
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.New()

	// Apply global middleware
	router.Use(sentryMonitor.GinMiddleware())
	router.Use(sentryMonitor.RecoveryMiddleware())
	router.Use(libmiddleware.LoggerMiddleware(logger))

	// CORS - use environment-based configuration
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001,http://localhost:3002,http://localhost:3003")
	router.Use(libmiddleware.CORSWithOrigins(allowedOrigins))

	// Security headers
	router.Use(libmiddleware.SecurityHeaders())

	// Input validation
	router.Use(libmiddleware.InputValidation())

	// Rate limiting (50 requests per minute)
	rateLimiter := libmiddleware.NewRateLimiter(50, 100)
	rateLimiter.CleanupLimiters()

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "marketplace",
			"time":    time.Now().UTC(),
		})
	})

	// Setup routes using the routes package
	routes.SetupRoutes(router, &routes.RouteConfig{
		ConnectionHandler: connectionHandler,
		ProductHandler:    productHandler,
		CategoryHandler:   categoryHandler,
		InventoryHandler:  inventoryHandler,
		OrderHandler:      orderHandler,
		WebhookHandler:    webhookHandler,
		AnalyticsHandler:  analyticsHandler,
		JWTManager:        jwtManager,
	})

	// Create server
	srv := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("🚀 Marketplace service starting on port " + cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
