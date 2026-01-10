package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/clients"
	"github.com/niaga-platform/service-marketplace/internal/events"
	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

// MarketplaceSyncHandler handles syncing products to connected marketplaces
// when products are updated in the admin panel.
type MarketplaceSyncHandler struct {
	connectionRepo      *repository.ConnectionRepository
	productMappingRepo  *repository.ProductMappingRepository
	categoryMappingRepo *repository.CategoryMappingRepository
	catalogClient       *clients.CatalogClient
	eventPublisher      *events.Publisher
	encryptor           *utils.Encryptor
	logger              *zap.Logger

	// Shopee configuration
	shopeePartnerID  string
	shopeePartnerKey string
	shopeeSandbox    bool

	// Auto-sync settings
	autoSyncEnabled bool
}

// MarketplaceSyncHandlerConfig holds configuration for the sync handler
type MarketplaceSyncHandlerConfig struct {
	ShopeePartnerID  string
	ShopeePartnerKey string
	ShopeeSandbox    bool
	EncryptionKey    string
	AutoSyncEnabled  bool
}

// NewMarketplaceSyncHandler creates a new marketplace sync handler
func NewMarketplaceSyncHandler(
	connectionRepo *repository.ConnectionRepository,
	productMappingRepo *repository.ProductMappingRepository,
	categoryMappingRepo *repository.CategoryMappingRepository,
	catalogClient *clients.CatalogClient,
	eventPublisher *events.Publisher,
	cfg *MarketplaceSyncHandlerConfig,
	logger *zap.Logger,
) (*MarketplaceSyncHandler, error) {
	var encryptor *utils.Encryptor
	if cfg.EncryptionKey != "" {
		var err error
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			logger.Warn("Failed to initialize encryptor", zap.Error(err))
		}
	}

	return &MarketplaceSyncHandler{
		connectionRepo:      connectionRepo,
		productMappingRepo:  productMappingRepo,
		categoryMappingRepo: categoryMappingRepo,
		catalogClient:       catalogClient,
		eventPublisher:      eventPublisher,
		encryptor:           encryptor,
		logger:              logger,
		shopeePartnerID:     cfg.ShopeePartnerID,
		shopeePartnerKey:    cfg.ShopeePartnerKey,
		shopeeSandbox:       cfg.ShopeeSandbox,
		autoSyncEnabled:     cfg.AutoSyncEnabled,
	}, nil
}

// HandleStockChanged handles inventory stock change events
func (h *MarketplaceSyncHandler) HandleStockChanged(event *events.StockChangedEvent) error {
	if !h.autoSyncEnabled {
		h.logger.Debug("Auto-sync disabled, skipping stock changed event")
		return nil
	}

	ctx := context.Background()

	// Find all product mappings for this product
	mappings, err := h.productMappingRepo.GetByInternalProductID(ctx, event.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get product mappings: %w", err)
	}

	if len(mappings) == 0 {
		h.logger.Debug("No marketplace mappings found for product", zap.String("product_id", event.ProductID.String()))
		return nil
	}

	// Sync inventory to each connected marketplace
	for _, mapping := range mappings {
		if err := h.syncInventoryToMarketplace(ctx, &mapping, event.NewQuantity); err != nil {
			h.logger.Error("Failed to sync inventory to marketplace",
				zap.String("connection_id", mapping.ConnectionID.String()),
				zap.String("product_id", event.ProductID.String()),
				zap.Error(err),
			)
			continue
		}
	}

	return nil
}

// HandleProductUpdated handles product update events from catalog service
func (h *MarketplaceSyncHandler) HandleProductUpdated(event *events.ProductUpdatedEvent) error {
	if !h.autoSyncEnabled {
		h.logger.Debug("Auto-sync disabled, skipping product updated event")
		return nil
	}

	ctx := context.Background()

	productID, err := uuid.Parse(event.ProductID)
	if err != nil {
		return fmt.Errorf("invalid product ID: %w", err)
	}

	// Find all product mappings for this product
	mappings, err := h.productMappingRepo.GetByInternalProductID(ctx, productID)
	if err != nil {
		return fmt.Errorf("failed to get product mappings: %w", err)
	}

	if len(mappings) == 0 {
		h.logger.Debug("No marketplace mappings found for product", zap.String("product_id", event.ProductID))
		return nil
	}

	h.logger.Info("Syncing product update to marketplaces",
		zap.String("product_id", event.ProductID),
		zap.Int("marketplace_count", len(mappings)),
	)

	// Fetch full product details from catalog
	products, err := h.catalogClient.GetProducts(ctx, []string{event.ProductID})
	if err != nil || len(products) == 0 {
		return fmt.Errorf("failed to get product details from catalog: %w", err)
	}
	product := products[0]

	// Sync to each connected marketplace
	for _, mapping := range mappings {
		go h.syncProductUpdateToMarketplace(ctx, &mapping, &product)
	}

	return nil
}

// HandleProductDeleted handles product deletion events
func (h *MarketplaceSyncHandler) HandleProductDeleted(event *events.ProductDeletedEvent) error {
	if !h.autoSyncEnabled {
		h.logger.Debug("Auto-sync disabled, skipping product deleted event")
		return nil
	}

	ctx := context.Background()

	productID, err := uuid.Parse(event.ProductID)
	if err != nil {
		return fmt.Errorf("invalid product ID: %w", err)
	}

	// Find all product mappings for this product
	mappings, err := h.productMappingRepo.GetByInternalProductID(ctx, productID)
	if err != nil {
		return fmt.Errorf("failed to get product mappings: %w", err)
	}

	if len(mappings) == 0 {
		return nil
	}

	h.logger.Info("Processing product deletion for marketplaces",
		zap.String("product_id", event.ProductID),
		zap.Int("marketplace_count", len(mappings)),
	)

	// Delete from each connected marketplace
	for _, mapping := range mappings {
		go h.deleteProductFromMarketplace(ctx, &mapping)
	}

	return nil
}

// syncProductUpdateToMarketplace syncs a product update to a specific marketplace
func (h *MarketplaceSyncHandler) syncProductUpdateToMarketplace(ctx context.Context, mapping *models.ProductMapping, product *clients.Product) {
	// Get connection details
	conn, err := h.connectionRepo.GetByID(ctx, mapping.ConnectionID)
	if err != nil || !conn.IsActive {
		h.logger.Warn("Connection not found or inactive",
			zap.String("connection_id", mapping.ConnectionID.String()),
		)
		return
	}

	// Mark as syncing
	h.productMappingRepo.UpdateSyncStatus(ctx, mapping.ID, models.SyncStatusPending, "")

	var syncErr error
	defer func() {
		if syncErr != nil {
			h.productMappingRepo.UpdateSyncStatus(ctx, mapping.ID, models.SyncStatusError, syncErr.Error())
			if h.eventPublisher != nil {
				h.eventPublisher.PublishSyncFailed(&events.SyncFailedEvent{
					ConnectionID: mapping.ConnectionID,
					Platform:     conn.Platform,
					ProductID:    mapping.InternalProductID,
					SyncType:     "product",
					Error:        syncErr.Error(),
					Timestamp:    time.Now(),
				})
			}
		} else {
			h.productMappingRepo.UpdateSyncStatus(ctx, mapping.ID, models.SyncStatusSynced, "")
			if h.eventPublisher != nil {
				h.eventPublisher.PublishSyncCompleted(&events.SyncCompletedEvent{
					ConnectionID: mapping.ConnectionID,
					Platform:     conn.Platform,
					ProductID:    mapping.InternalProductID,
					SyncType:     "product",
					Timestamp:    time.Now(),
				})
			}
		}
	}()

	switch conn.Platform {
	case "shopee":
		syncErr = h.updateProductOnShopee(ctx, conn, mapping, product)
	default:
		syncErr = fmt.Errorf("unsupported platform: %s", conn.Platform)
	}
}

// updateProductOnShopee updates a product on Shopee
func (h *MarketplaceSyncHandler) updateProductOnShopee(ctx context.Context, conn *models.Connection, mapping *models.ProductMapping, product *clients.Product) error {
	// Decrypt access token
	accessToken := conn.AccessToken
	if h.encryptor != nil {
		var err error
		accessToken, err = h.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	// Create Shopee client
	client, err := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  h.shopeePartnerID,
		PartnerKey: h.shopeePartnerKey,
		IsSandbox:  h.shopeeSandbox,
		Logger:     h.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create Shopee client: %w", err)
	}

	shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
	client.SetTokens(accessToken, shopID)

	productProvider := shopee.NewProductProvider(client)

	// Build update request
	price := product.BasePrice
	if product.SalePrice != nil {
		price = *product.SalePrice
	}

	updateReq := &providers.ProductUpdateRequest{
		Name:        product.Name,
		Description: product.Description,
		Price:       &price,
	}

	// Update on Shopee
	if err := productProvider.UpdateProduct(ctx, mapping.ExternalProductID, updateReq); err != nil {
		return fmt.Errorf("failed to update product on Shopee: %w", err)
	}

	h.logger.Info("Successfully synced product update to Shopee",
		zap.String("internal_product_id", mapping.InternalProductID.String()),
		zap.String("external_product_id", mapping.ExternalProductID),
		zap.String("shop_id", conn.ShopID),
	)

	return nil
}

// syncInventoryToMarketplace syncs inventory to a specific marketplace
func (h *MarketplaceSyncHandler) syncInventoryToMarketplace(ctx context.Context, mapping *models.ProductMapping, quantity int) error {
	conn, err := h.connectionRepo.GetByID(ctx, mapping.ConnectionID)
	if err != nil || !conn.IsActive {
		return fmt.Errorf("connection not found or inactive")
	}

	switch conn.Platform {
	case "shopee":
		return h.updateInventoryOnShopee(ctx, conn, mapping, quantity)
	default:
		return fmt.Errorf("unsupported platform: %s", conn.Platform)
	}
}

// updateInventoryOnShopee updates inventory on Shopee
func (h *MarketplaceSyncHandler) updateInventoryOnShopee(ctx context.Context, conn *models.Connection, mapping *models.ProductMapping, quantity int) error {
	accessToken := conn.AccessToken
	if h.encryptor != nil {
		var err error
		accessToken, err = h.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	client, err := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  h.shopeePartnerID,
		PartnerKey: h.shopeePartnerKey,
		IsSandbox:  h.shopeeSandbox,
		Logger:     h.logger,
	})
	if err != nil {
		return err
	}

	shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
	client.SetTokens(accessToken, shopID)

	productProvider := shopee.NewProductProvider(client)

	updates := []providers.InventoryUpdate{
		{
			ExternalProductID: mapping.ExternalProductID,
			Quantity:          quantity,
		},
	}

	if err := productProvider.UpdateInventory(ctx, updates); err != nil {
		return fmt.Errorf("failed to update inventory on Shopee: %w", err)
	}

	h.logger.Info("Successfully synced inventory to Shopee",
		zap.String("external_product_id", mapping.ExternalProductID),
		zap.Int("quantity", quantity),
	)

	return nil
}

// deleteProductFromMarketplace deletes a product from a specific marketplace
func (h *MarketplaceSyncHandler) deleteProductFromMarketplace(ctx context.Context, mapping *models.ProductMapping) {
	conn, err := h.connectionRepo.GetByID(ctx, mapping.ConnectionID)
	if err != nil || !conn.IsActive {
		h.logger.Warn("Connection not found or inactive for deletion",
			zap.String("connection_id", mapping.ConnectionID.String()),
		)
		return
	}

	var deleteErr error
	switch conn.Platform {
	case "shopee":
		deleteErr = h.deleteProductOnShopee(ctx, conn, mapping)
	default:
		deleteErr = fmt.Errorf("unsupported platform: %s", conn.Platform)
	}

	if deleteErr != nil {
		h.logger.Error("Failed to delete product from marketplace",
			zap.String("connection_id", mapping.ConnectionID.String()),
			zap.String("external_product_id", mapping.ExternalProductID),
			zap.Error(deleteErr),
		)
		return
	}

	// Remove the mapping after successful deletion
	if err := h.productMappingRepo.Delete(ctx, mapping.ID); err != nil {
		h.logger.Error("Failed to delete product mapping",
			zap.String("mapping_id", mapping.ID.String()),
			zap.Error(err),
		)
	}
}

// deleteProductOnShopee deletes a product from Shopee
func (h *MarketplaceSyncHandler) deleteProductOnShopee(ctx context.Context, conn *models.Connection, mapping *models.ProductMapping) error {
	accessToken := conn.AccessToken
	if h.encryptor != nil {
		var err error
		accessToken, err = h.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	client, err := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  h.shopeePartnerID,
		PartnerKey: h.shopeePartnerKey,
		IsSandbox:  h.shopeeSandbox,
		Logger:     h.logger,
	})
	if err != nil {
		return err
	}

	shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
	client.SetTokens(accessToken, shopID)

	productProvider := shopee.NewProductProvider(client)

	if err := productProvider.DeleteProduct(ctx, mapping.ExternalProductID); err != nil {
		return fmt.Errorf("failed to delete product on Shopee: %w", err)
	}

	h.logger.Info("Successfully deleted product from Shopee",
		zap.String("external_product_id", mapping.ExternalProductID),
	)

	return nil
}

// SetAutoSyncEnabled enables or disables auto-sync
func (h *MarketplaceSyncHandler) SetAutoSyncEnabled(enabled bool) {
	h.autoSyncEnabled = enabled
}

// IsAutoSyncEnabled returns whether auto-sync is enabled
func (h *MarketplaceSyncHandler) IsAutoSyncEnabled() bool {
	return h.autoSyncEnabled
}

// Ensure MarketplaceSyncHandler implements EventHandler
var _ events.EventHandler = (*MarketplaceSyncHandler)(nil)
