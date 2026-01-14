package shopee

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

const (
	PlatformName = "shopee"
)

// Provider implements the MarketplaceProvider interface for Shopee.
type Provider struct {
	client          *Client
	authProvider    *AuthProvider
	productProvider *ProductProvider
	orderProvider   *OrderProvider
	returnProvider  *ReturnProvider
	webhookHandler  *WebhookHandler
	logger          *zap.Logger
	config          *ProviderConfig
}

// ProviderConfig holds configuration for the Shopee provider.
type ProviderConfig struct {
	PartnerID      string
	PartnerKey     string
	RedirectURL    string
	WebhookURL     string
	IsSandbox      bool
	RequestTimeout time.Duration
}

// NewProvider creates a new Shopee marketplace provider.
func NewProvider(cfg *ProviderConfig, logger *zap.Logger) (*Provider, error) {
	if cfg.PartnerID == "" || cfg.PartnerKey == "" {
		return nil, fmt.Errorf("partner_id and partner_key are required")
	}

	client, err := NewClient(&ClientConfig{
		PartnerID:      cfg.PartnerID,
		PartnerKey:     cfg.PartnerKey,
		IsSandbox:      cfg.IsSandbox,
		RedirectURL:    cfg.RedirectURL,
		Logger:         logger,
		RequestTimeout: cfg.RequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Shopee client: %w", err)
	}

	return &Provider{
		client:          client,
		authProvider:    NewAuthProvider(client, cfg.RedirectURL),
		productProvider: NewProductProvider(client),
		orderProvider:   NewOrderProvider(client),
		returnProvider:  NewReturnProvider(client),
		webhookHandler:  NewWebhookHandler(cfg.PartnerKey, cfg.WebhookURL, logger),
		logger:          logger,
		config:          cfg,
	}, nil
}

// GetPlatform returns the platform identifier.
func (p *Provider) GetPlatform() string {
	return PlatformName
}

// SetCredentials configures the provider with shop-specific credentials.
func (p *Provider) SetCredentials(accessToken string, shopID int64) {
	p.client.SetTokens(accessToken, shopID)
}

// SetCredentialsWithRefresh configures the provider with full token management.
func (p *Provider) SetCredentialsWithRefresh(accessToken, refreshToken string, shopID int64, expiresAt time.Time, refresher TokenRefresher) {
	p.client.SetTokensWithRefresh(accessToken, refreshToken, shopID, expiresAt)
	p.client.SetTokenRefresher(refresher)
}

// --- OAuth Methods ---

// GetAuthURL generates the OAuth authorization URL.
func (p *Provider) GetAuthURL(state string) string {
	return p.authProvider.GetAuthURL(state)
}

// ExchangeCode exchanges an authorization code for tokens.
func (p *Provider) ExchangeCode(ctx context.Context, code string) (*providers.TokenResponse, error) {
	// For Shopee, shop_id comes in the callback, so we need to pass 0 here
	// The actual implementation uses the shop_id from the callback
	return nil, fmt.Errorf("use ExchangeCodeWithShopID for Shopee")
}

// ExchangeCodeWithShopID exchanges an authorization code for tokens with shop ID.
func (p *Provider) ExchangeCodeWithShopID(ctx context.Context, code string, shopID int64) (*providers.TokenResponse, error) {
	return p.authProvider.ExchangeCode(ctx, code, shopID)
}

// RefreshToken refreshes an expired access token.
func (p *Provider) RefreshToken(ctx context.Context, refreshToken string) (*providers.TokenResponse, error) {
	return nil, fmt.Errorf("use RefreshTokenWithShopID for Shopee")
}

// RefreshTokenWithShopID refreshes a token with the shop ID.
func (p *Provider) RefreshTokenWithShopID(ctx context.Context, refreshToken string, shopID int64) (*providers.TokenResponse, error) {
	return p.authProvider.RefreshToken(ctx, refreshToken, shopID)
}

// --- Shop Info ---

// GetShopInfo retrieves shop information.
func (p *Provider) GetShopInfo(ctx context.Context) (*providers.ShopInfo, error) {
	return p.authProvider.GetShopInfo(ctx)
}

// --- Product Methods ---

// GetCategories retrieves marketplace categories.
func (p *Provider) GetCategories(ctx context.Context) ([]providers.ExternalCategory, error) {
	return p.productProvider.GetCategories(ctx)
}

// PushProduct creates a new product on the marketplace.
func (p *Provider) PushProduct(ctx context.Context, product *providers.ProductPushRequest) (*providers.ProductPushResponse, error) {
	return p.productProvider.PushProduct(ctx, product)
}

// UpdateProduct updates an existing product.
func (p *Provider) UpdateProduct(ctx context.Context, externalID string, product *providers.ProductUpdateRequest) error {
	return p.productProvider.UpdateProduct(ctx, externalID, product)
}

// DeleteProduct deletes a product from the marketplace.
func (p *Provider) DeleteProduct(ctx context.Context, externalID string) error {
	return p.productProvider.DeleteProduct(ctx, externalID)
}

// --- Inventory Methods ---

// UpdateInventory updates stock levels for products.
func (p *Provider) UpdateInventory(ctx context.Context, updates []providers.InventoryUpdate) error {
	return p.productProvider.UpdateInventory(ctx, updates)
}

// GetInventory retrieves current inventory levels.
func (p *Provider) GetInventory(ctx context.Context, externalProductIDs []string) ([]providers.InventoryItem, error) {
	return p.productProvider.GetInventory(ctx, externalProductIDs)
}

// --- Order Methods ---

// GetOrders retrieves orders from the marketplace.
func (p *Provider) GetOrders(ctx context.Context, params providers.OrderQueryParams) ([]providers.ExternalOrder, error) {
	orderParams := &providers.OrderListParams{
		Status:   params.Status,
		PageSize: params.PageSize,
	}
	if params.StartTime != nil {
		orderParams.TimeFrom = *params.StartTime
	} else {
		orderParams.TimeFrom = time.Now().AddDate(0, 0, -7) // Default to last 7 days
	}
	if params.EndTime != nil {
		orderParams.TimeTo = *params.EndTime
	} else {
		orderParams.TimeTo = time.Now()
	}
	if orderParams.PageSize == 0 {
		orderParams.PageSize = 50
	}

	orders, _, err := p.orderProvider.GetOrders(ctx, orderParams)
	return orders, err
}

// GetOrder retrieves a single order.
func (p *Provider) GetOrder(ctx context.Context, externalOrderID string) (*providers.ExternalOrder, error) {
	return p.orderProvider.GetOrder(ctx, externalOrderID)
}

// UpdateOrderStatus updates the status of an order.
func (p *Provider) UpdateOrderStatus(ctx context.Context, externalOrderID string, status string, tracking *providers.TrackingInfo) error {
	return p.orderProvider.UpdateOrderStatus(ctx, externalOrderID, status)
}

// --- Return Methods ---

// GetReturns retrieves return/refund requests from the marketplace.
func (p *Provider) GetReturns(ctx context.Context, params *providers.ReturnListParams) ([]providers.ExternalReturn, string, error) {
	return p.returnProvider.GetReturns(ctx, params)
}

// GetReturn retrieves a single return request.
func (p *Provider) GetReturn(ctx context.Context, externalReturnID string) (*providers.ExternalReturn, error) {
	return p.returnProvider.GetReturn(ctx, externalReturnID)
}

// ConfirmReturn accepts a return request.
func (p *Provider) ConfirmReturn(ctx context.Context, externalReturnID string) error {
	return p.returnProvider.ConfirmReturn(ctx, externalReturnID)
}

// DisputeReturn disputes/rejects a return request.
func (p *Provider) DisputeReturn(ctx context.Context, externalReturnID string, email string, reason string, images []string) error {
	return p.returnProvider.DisputeReturn(ctx, externalReturnID, email, reason, images)
}

// --- Webhook Methods ---

// VerifyWebhook verifies the signature of an incoming webhook.
func (p *Provider) VerifyWebhook(ctx context.Context, body []byte, headers map[string]string) (bool, error) {
	return p.webhookHandler.VerifyWebhook(ctx, body, headers)
}

// ParseWebhookEvent parses a raw webhook body into a structured event.
func (p *Provider) ParseWebhookEvent(body []byte) (*providers.WebhookEvent, error) {
	return p.webhookHandler.ParseWebhookEvent(body)
}

// --- Utility Methods ---

// GetClient returns the underlying Shopee client for advanced usage.
func (p *Provider) GetClient() *Client {
	return p.client
}

// ParseShopID parses a string shop ID to int64.
func ParseShopID(shopIDStr string) (int64, error) {
	return strconv.ParseInt(shopIDStr, 10, 64)
}

// Ensure Provider implements MarketplaceProvider.
var _ providers.MarketplaceProvider = (*Provider)(nil)
