package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/clients"
	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/providers/tiktok"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

var (
	ErrCategoryMappingNotFound = errors.New("category mapping not found")
	ErrProductMappingNotFound  = errors.New("product mapping not found")
	ErrNoProductsToSync        = errors.New("no products to sync")
)

// ProductSyncService handles product synchronization
type ProductSyncService struct {
	connectionRepo      *repository.ConnectionRepository
	productMappingRepo  *repository.ProductMappingRepository
	categoryMappingRepo *repository.CategoryMappingRepository
	syncJobRepo         *repository.SyncJobRepository
	catalogClient       *clients.CatalogClient
	encryptor           *utils.Encryptor
	logger              *zap.Logger

	// Provider factories
	shopeeClientFactory func(accessToken string, shopID int64) (*shopee.Client, *shopee.ProductProvider)
	tiktokClientFactory func(accessToken, shopID string) (*tiktok.Client, *tiktok.ProductProvider)
}

// ProductSyncServiceConfig holds configuration for ProductSyncService
type ProductSyncServiceConfig struct {
	ShopeePartnerID  string
	ShopeePartnerKey string
	ShopeeSandbox    bool
	TikTokAppKey     string
	TikTokAppSecret  string
	EncryptionKey    string
}

// NewProductSyncService creates a new ProductSyncService
func NewProductSyncService(
	connectionRepo *repository.ConnectionRepository,
	productMappingRepo *repository.ProductMappingRepository,
	categoryMappingRepo *repository.CategoryMappingRepository,
	syncJobRepo *repository.SyncJobRepository,
	catalogClient *clients.CatalogClient,
	cfg *ProductSyncServiceConfig,
	logger *zap.Logger,
) (*ProductSyncService, error) {
	var encryptor *utils.Encryptor
	if cfg.EncryptionKey != "" {
		var err error
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			logger.Warn("Failed to initialize encryptor", zap.Error(err))
		}
	}

	svc := &ProductSyncService{
		connectionRepo:      connectionRepo,
		productMappingRepo:  productMappingRepo,
		categoryMappingRepo: categoryMappingRepo,
		syncJobRepo:         syncJobRepo,
		catalogClient:       catalogClient,
		encryptor:           encryptor,
		logger:              logger,
	}

	// Set up provider factories
	svc.shopeeClientFactory = func(accessToken string, shopID int64) (*shopee.Client, *shopee.ProductProvider) {
		client, _ := shopee.NewClient(&shopee.ClientConfig{
			PartnerID:  cfg.ShopeePartnerID,
			PartnerKey: cfg.ShopeePartnerKey,
			IsSandbox:  cfg.ShopeeSandbox,
			Logger:     logger,
		})
		client.SetTokens(accessToken, shopID)
		return client, shopee.NewProductProvider(client)
	}

	svc.tiktokClientFactory = func(accessToken, shopID string) (*tiktok.Client, *tiktok.ProductProvider) {
		client := tiktok.NewClient(&tiktok.ClientConfig{
			AppKey:    cfg.TikTokAppKey,
			AppSecret: cfg.TikTokAppSecret,
			Logger:    logger,
		})
		client.SetTokens(accessToken, shopID)
		return client, tiktok.NewProductProvider(client)
	}

	return svc, nil
}

// GetMappedProducts retrieves product mappings for a connection
func (s *ProductSyncService) GetMappedProducts(ctx context.Context, connectionID uuid.UUID, filter *models.ProductMappingFilter) ([]models.ProductMapping, int64, error) {
	return s.productMappingRepo.GetByConnectionID(ctx, connectionID, filter)
}

// GetProductMapping retrieves a single product mapping
func (s *ProductSyncService) GetProductMapping(ctx context.Context, mappingID uuid.UUID) (*models.ProductMapping, error) {
	return s.productMappingRepo.GetByID(ctx, mappingID)
}

// GetExternalCategories fetches categories from the marketplace
func (s *ProductSyncService) GetExternalCategories(ctx context.Context, connectionID uuid.UUID) ([]providers.ExternalCategory, error) {
	conn, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, ErrConnectionNotFound
	}

	// Decrypt access token
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
		_, productProvider := s.shopeeClientFactory(accessToken, shopID)
		return productProvider.GetCategories(ctx)

	case "tiktok":
		_, productProvider := s.tiktokClientFactory(accessToken, conn.ShopID)
		return productProvider.GetCategories(ctx)

	default:
		return nil, ErrInvalidPlatform
	}
}

// GetCategoryMappings retrieves category mappings for a connection
func (s *ProductSyncService) GetCategoryMappings(ctx context.Context, connectionID uuid.UUID) ([]models.CategoryMapping, error) {
	return s.categoryMappingRepo.GetByConnectionID(ctx, connectionID)
}

// CreateCategoryMapping creates a new category mapping
func (s *ProductSyncService) CreateCategoryMapping(ctx context.Context, connectionID uuid.UUID, req *models.CreateCategoryMappingRequest) (*models.CategoryMapping, error) {
	// Verify connection exists
	_, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, ErrConnectionNotFound
	}

	// Check if mapping already exists
	existing, _ := s.categoryMappingRepo.GetByConnectionAndInternalCategory(ctx, connectionID, req.InternalCategoryID)
	if existing != nil {
		// Update existing
		existing.ExternalCategoryID = req.ExternalCategoryID
		existing.ExternalCategoryName = req.ExternalCategoryName
		if err := s.categoryMappingRepo.Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("failed to update mapping: %w", err)
		}
		return existing, nil
	}

	mapping := &models.CategoryMapping{
		ConnectionID:         connectionID,
		InternalCategoryID:   req.InternalCategoryID,
		ExternalCategoryID:   req.ExternalCategoryID,
		ExternalCategoryName: req.ExternalCategoryName,
	}

	if err := s.categoryMappingRepo.Create(ctx, mapping); err != nil {
		return nil, fmt.Errorf("failed to create mapping: %w", err)
	}

	return mapping, nil
}

// DeleteCategoryMapping deletes a category mapping
func (s *ProductSyncService) DeleteCategoryMapping(ctx context.Context, mappingID uuid.UUID) error {
	return s.categoryMappingRepo.Delete(ctx, mappingID)
}

// PushProducts pushes products to a marketplace
// If productIDs is empty, fetches all active products from catalog
func (s *ProductSyncService) PushProducts(ctx context.Context, connectionID uuid.UUID, productIDs []string) (*models.SyncJob, error) {
	conn, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, ErrConnectionNotFound
	}

	// If no specific products, fetch all from catalog
	pushAll := len(productIDs) == 0
	if pushAll {
		s.logger.Info("Push all products requested, fetching from catalog")
		products, err := s.catalogClient.GetAllProducts(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch products from catalog: %w", err)
		}
		if len(products) == 0 {
			return nil, ErrNoProductsToSync
		}
		productIDs = make([]string, len(products))
		for i, p := range products {
			productIDs[i] = p.ID
		}
		s.logger.Info("Fetched products for push all", zap.Int("count", len(productIDs)))
	}

	// Create sync job
	payload, _ := json.Marshal(map[string]interface{}{
		"product_ids": productIDs,
		"push_all":    pushAll,
	})

	job := &models.SyncJob{
		ConnectionID: connectionID,
		JobType:      models.JobTypeProductPush,
		Payload:      payload,
		Status:       models.JobStatusPending,
		MaxAttempts:  3,
	}

	if err := s.syncJobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create sync job: %w", err)
	}

	// Process immediately (in production, this would be done by a worker)
	go s.processProductPushJob(context.Background(), job, conn)

	return job, nil
}

// processProductPushJob processes a product push job
func (s *ProductSyncService) processProductPushJob(ctx context.Context, job *models.SyncJob, conn *models.Connection) {
	// Mark as processing
	if err := s.syncJobRepo.MarkProcessing(ctx, job.ID); err != nil {
		s.logger.Error("Failed to mark job as processing", zap.Error(err))
		return
	}

	// Parse payload
	var payload struct {
		ProductIDs []string `json:"product_ids"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		s.syncJobRepo.MarkFailed(ctx, job.ID, "invalid payload")
		return
	}

	// Decrypt access token
	accessToken := conn.AccessToken
	if s.encryptor != nil {
		var err error
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			s.syncJobRepo.MarkFailed(ctx, job.ID, "failed to decrypt token")
			return
		}
	}

	// Get product provider
	var pushProduct func(ctx context.Context, product *providers.ProductPushRequest) (*providers.ProductPushResponse, error)

	switch conn.Platform {
	case "shopee":
		shopID, _ := strconv.ParseInt(conn.ShopID, 10, 64)
		_, productProvider := s.shopeeClientFactory(accessToken, shopID)
		pushProduct = productProvider.PushProduct

	case "tiktok":
		_, productProvider := s.tiktokClientFactory(accessToken, conn.ShopID)
		pushProduct = productProvider.PushProduct

	default:
		s.syncJobRepo.MarkFailed(ctx, job.ID, "invalid platform")
		return
	}

	// Fetch products from catalog
	products, err := s.catalogClient.GetProducts(ctx, payload.ProductIDs)
	if err != nil {
		s.syncJobRepo.MarkFailed(ctx, job.ID, fmt.Sprintf("failed to fetch products: %v", err))
		return
	}

	// Push each product
	successCount := 0
	for _, product := range products {
		// Get category mapping
		internalCatID, _ := uuid.Parse(product.CategoryID)
		catMapping, err := s.categoryMappingRepo.GetByConnectionAndInternalCategory(ctx, job.ConnectionID, internalCatID)
		if err != nil {
			s.logger.Warn("No category mapping for product", zap.String("product", product.ID))
			continue
		}

		// Build push request
		images := make([]string, len(product.Images))
		for i, img := range product.Images {
			images[i] = img.URL
		}

		price := product.BasePrice
		if product.SalePrice != nil {
			price = *product.SalePrice
		}

		// Map dimensions if available from catalog
		var dimensions *providers.Dimensions
		if product.Dimensions != nil {
			dimensions = &providers.Dimensions{
				Length: product.Dimensions.Length,
				Width:  product.Dimensions.Width,
				Height: product.Dimensions.Height,
			}
		}

		pushReq := &providers.ProductPushRequest{
			InternalID:    product.ID,
			Name:          product.Name,
			Description:   product.Description,
			Price:         price,
			OriginalPrice: product.BasePrice,
			Stock:         product.StockQuantity,
			SKU:           product.SKU,
			CategoryID:    catMapping.ExternalCategoryID,
			Images:        images,
			Weight:        product.Weight,
			Brand:         product.Brand,
			Dimensions:    dimensions,
		}

		// Push to marketplace
		resp, err := pushProduct(ctx, pushReq)
		if err != nil {
			s.logger.Error("Failed to push product", zap.String("product", product.ID), zap.Error(err))

			// Create/update mapping with error
			productID, _ := uuid.Parse(product.ID)
			mapping := &models.ProductMapping{
				ConnectionID:      job.ConnectionID,
				InternalProductID: productID,
				SyncStatus:        models.SyncStatusError,
				SyncError:         err.Error(),
			}
			s.productMappingRepo.Create(ctx, mapping)
			continue
		}

		// Create/update product mapping
		productID, _ := uuid.Parse(product.ID)
		mapping := &models.ProductMapping{
			ConnectionID:      job.ConnectionID,
			InternalProductID: productID,
			ExternalProductID: resp.ExternalProductID,
			ExternalSKU:       resp.ExternalSKU,
			SyncStatus:        models.SyncStatusSynced,
		}

		existing, _ := s.productMappingRepo.GetByConnectionAndInternalProduct(ctx, job.ConnectionID, productID)
		if existing != nil {
			existing.ExternalProductID = resp.ExternalProductID
			existing.ExternalSKU = resp.ExternalSKU
			existing.SyncStatus = models.SyncStatusSynced
			existing.SyncError = ""
			s.productMappingRepo.Update(ctx, existing)
		} else {
			s.productMappingRepo.Create(ctx, mapping)
		}

		successCount++
	}

	// Mark job complete
	if successCount == len(products) {
		s.syncJobRepo.MarkCompleted(ctx, job.ID)
	} else if successCount > 0 {
		s.syncJobRepo.MarkCompleted(ctx, job.ID)
	} else {
		s.syncJobRepo.MarkFailed(ctx, job.ID, "no products were pushed successfully")
	}
}

// UpdateProductMapping updates a product mapping
func (s *ProductSyncService) UpdateProductMapping(ctx context.Context, mappingID uuid.UUID, status string) error {
	mapping, err := s.productMappingRepo.GetByID(ctx, mappingID)
	if err != nil {
		return ErrProductMappingNotFound
	}

	mapping.SyncStatus = status
	return s.productMappingRepo.Update(ctx, mapping)
}

// DeleteProductMapping deletes a product mapping
func (s *ProductSyncService) DeleteProductMapping(ctx context.Context, mappingID uuid.UUID) error {
	return s.productMappingRepo.Delete(ctx, mappingID)
}
