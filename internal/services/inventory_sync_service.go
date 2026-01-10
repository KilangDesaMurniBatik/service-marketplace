package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/events"
	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/providers/tiktok"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

var (
	ErrNoMappingFound = errors.New("no product mapping found for this product")
)

// InventorySyncService handles inventory synchronization
type InventorySyncService struct {
	connectionRepo     *repository.ConnectionRepository
	productMappingRepo *repository.ProductMappingRepository
	encryptor          *utils.Encryptor
	publisher          *events.Publisher
	logger             *zap.Logger

	shopeePartnerID  string
	shopeePartnerKey string
	shopeeSandbox    bool
	tiktokAppKey     string
	tiktokAppSecret  string
}

// InventorySyncServiceConfig holds configuration
type InventorySyncServiceConfig struct {
	ShopeePartnerID  string
	ShopeePartnerKey string
	ShopeeSandbox    bool
	TikTokAppKey     string
	TikTokAppSecret  string
	EncryptionKey    string
}

// NewInventorySyncService creates a new InventorySyncService
func NewInventorySyncService(
	connectionRepo *repository.ConnectionRepository,
	productMappingRepo *repository.ProductMappingRepository,
	publisher *events.Publisher,
	cfg *InventorySyncServiceConfig,
	logger *zap.Logger,
) (*InventorySyncService, error) {
	var encryptor *utils.Encryptor
	if cfg.EncryptionKey != "" {
		var err error
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			logger.Warn("Failed to initialize encryptor", zap.Error(err))
		}
	}

	return &InventorySyncService{
		connectionRepo:     connectionRepo,
		productMappingRepo: productMappingRepo,
		encryptor:          encryptor,
		publisher:          publisher,
		logger:             logger,
		shopeePartnerID:    cfg.ShopeePartnerID,
		shopeePartnerKey:   cfg.ShopeePartnerKey,
		shopeeSandbox:      cfg.ShopeeSandbox,
		tiktokAppKey:       cfg.TikTokAppKey,
		tiktokAppSecret:    cfg.TikTokAppSecret,
	}, nil
}

// HandleStockChanged implements events.EventHandler
func (s *InventorySyncService) HandleStockChanged(event *events.StockChangedEvent) error {
	ctx := context.Background()

	// Find all product mappings for this product
	mappings, err := s.productMappingRepo.GetByInternalProductID(ctx, event.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get mappings: %w", err)
	}

	if len(mappings) == 0 {
		s.logger.Debug("No marketplace mappings for product", zap.String("product_id", event.ProductID.String()))
		return nil
	}

	// Update each marketplace
	for _, mapping := range mappings {
		go s.syncInventoryForMapping(ctx, &mapping, event.NewQuantity)
	}

	return nil
}

// HandleProductUpdated implements events.EventHandler
// Note: For full product sync support, use MarketplaceSyncHandler instead
func (s *InventorySyncService) HandleProductUpdated(event *events.ProductUpdatedEvent) error {
	s.logger.Debug("Product updated event received by InventorySyncService",
		zap.String("product_id", event.ProductID),
		zap.String("note", "Use MarketplaceSyncHandler for full product sync"),
	)
	return nil
}

// HandleProductDeleted implements events.EventHandler
// Note: For full product sync support, use MarketplaceSyncHandler instead
func (s *InventorySyncService) HandleProductDeleted(event *events.ProductDeletedEvent) error {
	s.logger.Debug("Product deleted event received by InventorySyncService",
		zap.String("product_id", event.ProductID),
		zap.String("note", "Use MarketplaceSyncHandler for full product sync"),
	)
	return nil
}

// syncInventoryForMapping syncs inventory to a single marketplace
func (s *InventorySyncService) syncInventoryForMapping(ctx context.Context, mapping *models.ProductMapping, quantity int) {
	// Get connection
	conn, err := s.connectionRepo.GetByID(ctx, mapping.ConnectionID)
	if err != nil {
		s.logger.Error("Failed to get connection", zap.Error(err))
		return
	}

	if !conn.IsActive {
		s.logger.Debug("Connection is inactive, skipping", zap.String("connection_id", conn.ID.String()))
		return
	}

	// Decrypt token
	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			s.logger.Error("Failed to decrypt token", zap.Error(err))
			s.publishSyncFailed(conn, mapping, "failed to decrypt token")
			return
		}
	}

	// Update marketplace
	switch conn.Platform {
	case "shopee":
		err = s.updateShopeeInventory(ctx, conn, mapping, accessToken, quantity)
	case "tiktok":
		err = s.updateTikTokInventory(ctx, conn, mapping, accessToken, quantity)
	default:
		s.logger.Error("Unknown platform", zap.String("platform", conn.Platform))
		return
	}

	if err != nil {
		s.logger.Error("Failed to sync inventory",
			zap.String("platform", conn.Platform),
			zap.String("product_id", mapping.InternalProductID.String()),
			zap.Error(err),
		)
		s.publishSyncFailed(conn, mapping, err.Error())
		return
	}

	s.logger.Info("Inventory synced successfully",
		zap.String("platform", conn.Platform),
		zap.String("external_product_id", mapping.ExternalProductID),
		zap.Int("quantity", quantity),
	)

	s.publishSyncCompleted(conn, mapping)
}

func (s *InventorySyncService) updateShopeeInventory(ctx context.Context, conn *models.Connection, mapping *models.ProductMapping, accessToken string, quantity int) error {
	shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)

	client, _ := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  s.shopeePartnerID,
		PartnerKey: s.shopeePartnerKey,
		IsSandbox:  s.shopeeSandbox,
		Logger:     s.logger,
	})
	client.SetTokens(accessToken, shopID)

	provider := shopee.NewInventoryProvider(client)
	return provider.UpdateStock(ctx, mapping.ExternalProductID, quantity)
}

func (s *InventorySyncService) updateTikTokInventory(ctx context.Context, conn *models.Connection, mapping *models.ProductMapping, accessToken string, quantity int) error {
	client := tiktok.NewClient(&tiktok.ClientConfig{
		AppKey:    s.tiktokAppKey,
		AppSecret: s.tiktokAppSecret,
		Logger:    s.logger,
	})
	client.SetTokens(accessToken, conn.ShopID)

	provider := tiktok.NewInventoryProvider(client)
	return provider.UpdateStock(ctx, mapping.ExternalProductID, mapping.ExternalSKU, quantity)
}

func (s *InventorySyncService) publishSyncCompleted(conn *models.Connection, mapping *models.ProductMapping) {
	if s.publisher == nil {
		return
	}
	s.publisher.PublishSyncCompleted(&events.SyncCompletedEvent{
		ConnectionID: conn.ID,
		Platform:     conn.Platform,
		ProductID:    mapping.InternalProductID,
		SyncType:     "inventory",
		Timestamp:    time.Now(),
	})
}

func (s *InventorySyncService) publishSyncFailed(conn *models.Connection, mapping *models.ProductMapping, errMsg string) {
	if s.publisher == nil {
		return
	}
	s.publisher.PublishSyncFailed(&events.SyncFailedEvent{
		ConnectionID: conn.ID,
		Platform:     conn.Platform,
		ProductID:    mapping.InternalProductID,
		SyncType:     "inventory",
		Error:        errMsg,
		Timestamp:    time.Now(),
	})
}

// PushInventory manually pushes inventory for specific products
func (s *InventorySyncService) PushInventory(ctx context.Context, connectionID uuid.UUID, updates []providers.InventoryUpdate) ([]providers.InventoryUpdateResult, error) {
	conn, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, ErrConnectionNotFound
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	switch conn.Platform {
	case "shopee":
		shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
		client, _ := shopee.NewClient(&shopee.ClientConfig{
			PartnerID:  s.shopeePartnerID,
			PartnerKey: s.shopeePartnerKey,
			IsSandbox:  s.shopeeSandbox,
			Logger:     s.logger,
		})
		client.SetTokens(accessToken, shopID)
		provider := shopee.NewInventoryProvider(client)
		return provider.UpdateBatchStock(ctx, updates)

	case "tiktok":
		client := tiktok.NewClient(&tiktok.ClientConfig{
			AppKey:    s.tiktokAppKey,
			AppSecret: s.tiktokAppSecret,
			Logger:    s.logger,
		})
		client.SetTokens(accessToken, conn.ShopID)
		provider := tiktok.NewInventoryProvider(client)
		return provider.UpdateBatchStock(ctx, updates)

	default:
		return nil, ErrInvalidPlatform
	}
}

// GetInventoryStatus fetches current inventory from marketplace
func (s *InventorySyncService) GetInventoryStatus(ctx context.Context, connectionID uuid.UUID, externalProductIDs []string) ([]providers.InventoryItem, error) {
	conn, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, ErrConnectionNotFound
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	switch conn.Platform {
	case "shopee":
		shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
		client, _ := shopee.NewClient(&shopee.ClientConfig{
			PartnerID:  s.shopeePartnerID,
			PartnerKey: s.shopeePartnerKey,
			IsSandbox:  s.shopeeSandbox,
			Logger:     s.logger,
		})
		client.SetTokens(accessToken, shopID)
		provider := shopee.NewInventoryProvider(client)
		return provider.GetStock(ctx, externalProductIDs)

	case "tiktok":
		client := tiktok.NewClient(&tiktok.ClientConfig{
			AppKey:    s.tiktokAppKey,
			AppSecret: s.tiktokAppSecret,
			Logger:    s.logger,
		})
		client.SetTokens(accessToken, conn.ShopID)
		provider := tiktok.NewInventoryProvider(client)
		return provider.GetStock(ctx, externalProductIDs)

	default:
		return nil, ErrInvalidPlatform
	}
}
