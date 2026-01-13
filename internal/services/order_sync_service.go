package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"

	"github.com/niaga-platform/service-marketplace/internal/clients"
	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/providers/tiktok"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

// OrderSyncService handles order synchronization
type OrderSyncService struct {
	connectionRepo *repository.ConnectionRepository
	orderRepo      *repository.MarketplaceOrderRepository
	orderClient    *clients.OrderClient
	encryptor      *utils.Encryptor
	logger         *zap.Logger

	shopeePartnerID  string
	shopeePartnerKey string
	shopeeSandbox    bool
	tiktokAppKey     string
	tiktokAppSecret  string
}

// OrderSyncServiceConfig holds configuration
type OrderSyncServiceConfig struct {
	ShopeePartnerID  string
	ShopeePartnerKey string
	ShopeeSandbox    bool
	TikTokAppKey     string
	TikTokAppSecret  string
	EncryptionKey    string
}

// NewOrderSyncService creates a new OrderSyncService
func NewOrderSyncService(
	connectionRepo *repository.ConnectionRepository,
	orderRepo *repository.MarketplaceOrderRepository,
	orderClient *clients.OrderClient,
	cfg *OrderSyncServiceConfig,
	logger *zap.Logger,
) (*OrderSyncService, error) {
	var encryptor *utils.Encryptor
	if cfg.EncryptionKey != "" {
		var err error
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			logger.Warn("Failed to initialize encryptor", zap.Error(err))
		}
	}

	return &OrderSyncService{
		connectionRepo:   connectionRepo,
		orderRepo:        orderRepo,
		orderClient:      orderClient,
		encryptor:        encryptor,
		logger:           logger,
		shopeePartnerID:  cfg.ShopeePartnerID,
		shopeePartnerKey: cfg.ShopeePartnerKey,
		shopeeSandbox:    cfg.ShopeeSandbox,
		tiktokAppKey:     cfg.TikTokAppKey,
		tiktokAppSecret:  cfg.TikTokAppSecret,
	}, nil
}

// GetOrders retrieves marketplace orders for a connection
func (s *OrderSyncService) GetOrders(ctx context.Context, connectionID uuid.UUID, filter *models.MarketplaceOrderFilter) ([]models.MarketplaceOrder, int64, error) {
	return s.orderRepo.GetByConnectionID(ctx, connectionID, filter)
}

// SyncOrders manually syncs orders from marketplace
func (s *OrderSyncService) SyncOrders(ctx context.Context, connectionID uuid.UUID, timeFrom, timeTo time.Time) (int, error) {
	conn, err := s.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return 0, ErrConnectionNotFound
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return 0, fmt.Errorf("failed to decrypt token: %w", err)
		}
	}

	var orders []providers.ExternalOrder
	cursor := ""

	for {
		var fetchedOrders []providers.ExternalOrder
		var nextCursor string

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
			provider := shopee.NewOrderProvider(client)
			fetchedOrders, nextCursor, err = provider.GetOrders(ctx, &providers.OrderListParams{
				TimeFrom: timeFrom,
				TimeTo:   timeTo,
				PageSize: 50,
				Cursor:   cursor,
			})

		case "tiktok":
			client := tiktok.NewClient(&tiktok.ClientConfig{
				AppKey:    s.tiktokAppKey,
				AppSecret: s.tiktokAppSecret,
				Logger:    s.logger,
			})
			client.SetTokens(accessToken, conn.ShopID)
			provider := tiktok.NewOrderProvider(client)
			fetchedOrders, nextCursor, err = provider.GetOrders(ctx, &providers.OrderListParams{
				TimeFrom: timeFrom,
				TimeTo:   timeTo,
				PageSize: 50,
				Cursor:   cursor,
			})

		default:
			return 0, ErrInvalidPlatform
		}

		if err != nil {
			return len(orders), fmt.Errorf("failed to fetch orders: %w", err)
		}

		orders = append(orders, fetchedOrders...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Import orders
	importedCount := 0
	for _, order := range orders {
		if err := s.importOrder(ctx, conn, &order); err != nil {
			s.logger.Error("Failed to import order", zap.String("order_id", order.ExternalOrderID), zap.Error(err))
			continue
		}
		importedCount++
	}

	return importedCount, nil
}

func (s *OrderSyncService) importOrder(ctx context.Context, conn *models.Connection, order *providers.ExternalOrder) error {
	// Check if order already exists
	existing, _ := s.orderRepo.GetByExternalOrderID(ctx, conn.ID, order.ExternalOrderID)
	if existing != nil {
		// Update existing order status
		existing.Status = order.Status
		// Update shipping info if tracking available
		if order.TrackingNumber != "" {
			shippingInfo := models.ShippingInfoJSON{
				RecipientName:  order.ShippingAddress.Name,
				Phone:          order.ShippingAddress.Phone,
				AddressLine1:   order.ShippingAddress.Address,
				City:           order.ShippingAddress.City,
				State:          order.ShippingAddress.State,
				PostalCode:     order.ShippingAddress.ZipCode,
				Country:        order.ShippingAddress.Country,
				TrackingNumber: order.TrackingNumber,
				Courier:        order.Carrier,
			}
			shippingJSON, _ := json.Marshal(shippingInfo)
			existing.ShippingInfo = datatypes.JSON(shippingJSON)
		}
		return s.orderRepo.Update(ctx, existing)
	}

	// Build order data JSON
	orderItems := make([]models.OrderItemJSON, len(order.Items))
	for i, item := range order.Items {
		orderItems[i] = models.OrderItemJSON{
			ExternalProductID: item.ExternalProductID,
			SKU:               item.ExternalSKU,
			Name:              item.Name,
			Quantity:          item.Quantity,
			UnitPrice:         item.UnitPrice,
			TotalPrice:        item.TotalPrice,
		}
	}
	orderData := models.OrderDataJSON{
		Items: orderItems,
	}
	orderDataJSON, _ := json.Marshal(orderData)

	// Build buyer info JSON
	buyerInfo := models.BuyerInfoJSON{
		Name:   order.BuyerName,
		UserID: order.BuyerID,
		Phone:  order.ShippingAddress.Phone,
	}
	buyerInfoJSON, _ := json.Marshal(buyerInfo)

	// Build shipping info JSON
	shippingInfo := models.ShippingInfoJSON{
		RecipientName:  order.ShippingAddress.Name,
		Phone:          order.ShippingAddress.Phone,
		AddressLine1:   order.ShippingAddress.Address,
		City:           order.ShippingAddress.City,
		State:          order.ShippingAddress.State,
		PostalCode:     order.ShippingAddress.ZipCode,
		Country:        order.ShippingAddress.Country,
		TrackingNumber: order.TrackingNumber,
		Courier:        order.Carrier,
	}
	shippingInfoJSON, _ := json.Marshal(shippingInfo)

	// Create marketplace order record
	now := time.Now()
	mpOrder := &models.MarketplaceOrder{
		ConnectionID:    conn.ID,
		ExternalOrderID: order.ExternalOrderID,
		Platform:        conn.Platform,
		Status:          order.Status,
		OrderData:       datatypes.JSON(orderDataJSON),
		BuyerInfo:       datatypes.JSON(buyerInfoJSON),
		ShippingInfo:    datatypes.JSON(shippingInfoJSON),
		TotalAmount:     order.TotalAmount,
		Currency:        order.Currency,
		SyncedAt:        &now,
	}

	if err := s.orderRepo.Create(ctx, mpOrder); err != nil {
		return fmt.Errorf("failed to create marketplace order: %w", err)
	}

	// Push to service-order if client configured
	if s.orderClient != nil {
		items := make([]clients.OrderItemRequest, len(order.Items))
		for i, item := range order.Items {
			items[i] = clients.OrderItemRequest{
				SKU:        item.ExternalSKU,
				Name:       item.Name,
				Quantity:   item.Quantity,
				UnitPrice:  item.UnitPrice,
				TotalPrice: item.TotalPrice,
			}
		}

		internalOrderID, err := s.orderClient.CreateOrder(ctx, &clients.CreateOrderRequest{
			ExternalOrderID: order.ExternalOrderID,
			Source:          conn.Platform,
			CustomerName:    order.BuyerName,
			CustomerPhone:   order.ShippingAddress.Phone,
			ShippingAddress: clients.AddressRequest{
				Name:       order.ShippingAddress.Name,
				Phone:      order.ShippingAddress.Phone,
				Address1:   order.ShippingAddress.Address,
				City:       order.ShippingAddress.City,
				State:      order.ShippingAddress.State,
				Country:    order.ShippingAddress.Country,
				PostalCode: order.ShippingAddress.ZipCode,
			},
			Items:       items,
			TotalAmount: order.TotalAmount,
			Currency:    order.Currency,
			Status:      order.Status,
			PaidAt:      order.PaidAt,
		})

		if err != nil {
			s.logger.Error("Failed to create order in service-order", zap.Error(err))
		} else {
			internalID, _ := uuid.Parse(internalOrderID)
			mpOrder.InternalOrderID = &internalID
			s.orderRepo.Update(ctx, mpOrder)
		}
	}

	return nil
}

// HandleShopeeOrderEvent handles webhook order events from Shopee
func (s *OrderSyncService) HandleShopeeOrderEvent(shopID int64, orderSN, status string) {
	ctx := context.Background()

	// Find connection by shop ID
	conn, err := s.connectionRepo.GetByPlatformAndShopID(ctx, "shopee", fmt.Sprintf("%d", shopID))
	if err != nil {
		s.logger.Error("Connection not found for shop", zap.Int64("shop_id", shopID))
		return
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, _ = s.encryptor.Decrypt(conn.AccessToken)
	}

	// Fetch order details
	client, _ := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  s.shopeePartnerID,
		PartnerKey: s.shopeePartnerKey,
		IsSandbox:  s.shopeeSandbox,
		Logger:     s.logger,
	})
	client.SetTokens(accessToken, shopID)
	provider := shopee.NewOrderProvider(client)

	order, err := provider.GetOrder(ctx, orderSN)
	if err != nil {
		s.logger.Error("Failed to fetch order", zap.String("order_sn", orderSN), zap.Error(err))
		return
	}

	if err := s.importOrder(ctx, conn, order); err != nil {
		s.logger.Error("Failed to import order from webhook", zap.Error(err))
	}
}

// HandleTikTokOrderEvent handles webhook order events from TikTok
func (s *OrderSyncService) HandleTikTokOrderEvent(shopID, orderID string, status int) {
	ctx := context.Background()

	// Find connection by shop ID
	conn, err := s.connectionRepo.GetByPlatformAndShopID(ctx, "tiktok", shopID)
	if err != nil {
		s.logger.Error("Connection not found for shop", zap.String("shop_id", shopID))
		return
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, _ = s.encryptor.Decrypt(conn.AccessToken)
	}

	// Fetch order details
	client := tiktok.NewClient(&tiktok.ClientConfig{
		AppKey:    s.tiktokAppKey,
		AppSecret: s.tiktokAppSecret,
		Logger:    s.logger,
	})
	client.SetTokens(accessToken, shopID)
	provider := tiktok.NewOrderProvider(client)

	order, err := provider.GetOrder(ctx, orderID)
	if err != nil {
		s.logger.Error("Failed to fetch order", zap.String("order_id", orderID), zap.Error(err))
		return
	}

	if err := s.importOrder(ctx, conn, order); err != nil {
		s.logger.Error("Failed to import order from webhook", zap.Error(err))
	}
}

// GetAWBDownloadURL gets the AWB download URL for an order
func (s *OrderSyncService) GetAWBDownloadURL(ctx context.Context, orderID uuid.UUID, documentType string) (string, error) {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return "", fmt.Errorf("order not found: %w", err)
	}

	conn, err := s.connectionRepo.GetByID(ctx, order.ConnectionID)
	if err != nil {
		return "", ErrConnectionNotFound
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt token: %w", err)
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
		provider := shopee.NewOrderProvider(client)

		// First, create the shipping document
		if err := provider.CreateShippingDocument(ctx, order.ExternalOrderID, documentType); err != nil {
			s.logger.Warn("Create shipping document warning", zap.Error(err))
			// Continue anyway - document might already exist
		}

		// Check if document is ready
		result, err := provider.GetShippingDocumentResult(ctx, order.ExternalOrderID, documentType)
		if err != nil {
			return "", fmt.Errorf("failed to get document status: %w", err)
		}

		if result.Status != "READY" {
			return "", fmt.Errorf("document not ready, status: %s", result.Status)
		}

		// Download the document URL
		url, err := provider.DownloadShippingDocument(ctx, order.ExternalOrderID, documentType)
		if err != nil {
			return "", fmt.Errorf("failed to get download URL: %w", err)
		}

		return url, nil

	case "tiktok":
		return "", fmt.Errorf("AWB download not yet implemented for TikTok")

	default:
		return "", ErrInvalidPlatform
	}
}

// UpdateOrderStatus updates order status on the marketplace
func (s *OrderSyncService) UpdateOrderStatus(ctx context.Context, orderID uuid.UUID, status string) error {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	conn, err := s.connectionRepo.GetByID(ctx, order.ConnectionID)
	if err != nil {
		return ErrConnectionNotFound
	}

	accessToken := conn.AccessToken
	if s.encryptor != nil {
		accessToken, err = s.encryptor.Decrypt(conn.AccessToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
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
		provider := shopee.NewOrderProvider(client)
		if err := provider.UpdateOrderStatus(ctx, order.ExternalOrderID, status); err != nil {
			return err
		}

	case "tiktok":
		client := tiktok.NewClient(&tiktok.ClientConfig{
			AppKey:    s.tiktokAppKey,
			AppSecret: s.tiktokAppSecret,
			Logger:    s.logger,
		})
		client.SetTokens(accessToken, conn.ShopID)
		provider := tiktok.NewOrderProvider(client)
		if err := provider.UpdateOrderStatus(ctx, order.ExternalOrderID, status); err != nil {
			return err
		}

	default:
		return ErrInvalidPlatform
	}

	// Update local record
	order.Status = status
	return s.orderRepo.Update(ctx, order)
}
