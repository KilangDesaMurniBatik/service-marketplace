package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ProviderFactory creates marketplace providers with proper token management.
type ProviderFactory struct {
	shopeeConfig   *ShopeeFactoryConfig
	tiktokConfig   *TikTokFactoryConfig
	tokenStore     TokenStore
	logger         *zap.Logger
}

// ShopeeFactoryConfig holds Shopee-specific configuration.
type ShopeeFactoryConfig struct {
	PartnerID      string
	PartnerKey     string
	RedirectURL    string
	WebhookURL     string
	IsSandbox      bool
	RequestTimeout time.Duration
}

// TikTokFactoryConfig holds TikTok-specific configuration.
type TikTokFactoryConfig struct {
	AppKey      string
	AppSecret   string
	RedirectURL string
}

// TokenStore defines the interface for token storage and retrieval.
type TokenStore interface {
	GetDecryptedTokens(ctx context.Context, connectionID uuid.UUID) (accessToken, refreshToken string, expiresAt *time.Time, err error)
	UpdateTokens(ctx context.Context, connectionID uuid.UUID, accessToken, refreshToken string, expiresAt time.Time) error
}

// ConnectionInfo contains information needed to create a provider for a connection.
type ConnectionInfo struct {
	ID           uuid.UUID
	Platform     string
	ShopID       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
}

// FactoryConfig holds configuration for the provider factory.
type FactoryConfig struct {
	Shopee     *ShopeeFactoryConfig
	TikTok     *TikTokFactoryConfig
	TokenStore TokenStore
	Logger     *zap.Logger
}

// NewProviderFactory creates a new provider factory.
func NewProviderFactory(cfg *FactoryConfig) *ProviderFactory {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &ProviderFactory{
		shopeeConfig: cfg.Shopee,
		tiktokConfig: cfg.TikTok,
		tokenStore:   cfg.TokenStore,
		logger:       logger,
	}
}

// CreateProvider creates a marketplace provider for the given platform.
// This creates an unauthenticated provider suitable for OAuth flows.
func (f *ProviderFactory) CreateProvider(platform string) (MarketplaceProvider, error) {
	switch platform {
	case "shopee":
		return f.createShopeeProvider()
	case "tiktok":
		return f.createTikTokProvider()
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// CreateProviderForConnection creates a provider configured for a specific connection.
// The provider will have tokens set and automatic refresh enabled.
func (f *ProviderFactory) CreateProviderForConnection(ctx context.Context, conn *ConnectionInfo) (MarketplaceProvider, error) {
	switch conn.Platform {
	case "shopee":
		return f.createShopeeProviderForConnection(ctx, conn)
	case "tiktok":
		return f.createTikTokProviderForConnection(ctx, conn)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", conn.Platform)
	}
}

// createShopeeProvider creates an unauthenticated Shopee provider.
func (f *ProviderFactory) createShopeeProvider() (MarketplaceProvider, error) {
	if f.shopeeConfig == nil {
		return nil, fmt.Errorf("Shopee configuration not provided")
	}

	// Import is handled by the caller - we return interface
	// The actual creation is done in the service layer where shopee package is imported
	return nil, fmt.Errorf("use CreateShopeeProvider from shopee package directly for unauthenticated provider")
}

// createShopeeProviderForConnection creates a Shopee provider for a specific connection.
func (f *ProviderFactory) createShopeeProviderForConnection(ctx context.Context, conn *ConnectionInfo) (MarketplaceProvider, error) {
	if f.shopeeConfig == nil {
		return nil, fmt.Errorf("Shopee configuration not provided")
	}

	// This method is a placeholder - actual implementation requires importing shopee package
	// which would create a circular dependency. The service layer should handle this.
	return nil, fmt.Errorf("createShopeeProviderForConnection should be implemented in service layer")
}

// createTikTokProvider creates an unauthenticated TikTok provider.
func (f *ProviderFactory) createTikTokProvider() (MarketplaceProvider, error) {
	if f.tiktokConfig == nil {
		return nil, fmt.Errorf("TikTok configuration not provided")
	}

	return nil, fmt.Errorf("use CreateTikTokProvider from tiktok package directly for unauthenticated provider")
}

// createTikTokProviderForConnection creates a TikTok provider for a specific connection.
func (f *ProviderFactory) createTikTokProviderForConnection(ctx context.Context, conn *ConnectionInfo) (MarketplaceProvider, error) {
	if f.tiktokConfig == nil {
		return nil, fmt.Errorf("TikTok configuration not provided")
	}

	return nil, fmt.Errorf("createTikTokProviderForConnection should be implemented in service layer")
}

// GetShopeeConfig returns the Shopee configuration.
func (f *ProviderFactory) GetShopeeConfig() *ShopeeFactoryConfig {
	return f.shopeeConfig
}

// GetTikTokConfig returns the TikTok configuration.
func (f *ProviderFactory) GetTikTokConfig() *TikTokFactoryConfig {
	return f.tiktokConfig
}

// IsShopeeConfigured returns true if Shopee is configured.
func (f *ProviderFactory) IsShopeeConfigured() bool {
	return f.shopeeConfig != nil && f.shopeeConfig.PartnerID != "" && f.shopeeConfig.PartnerKey != ""
}

// IsTikTokConfigured returns true if TikTok is configured.
func (f *ProviderFactory) IsTikTokConfigured() bool {
	return f.tiktokConfig != nil && f.tiktokConfig.AppKey != "" && f.tiktokConfig.AppSecret != ""
}
