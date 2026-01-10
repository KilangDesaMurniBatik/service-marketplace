package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/providers/tiktok"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

// ProviderFactoryService creates marketplace providers with proper configuration.
type ProviderFactoryService struct {
	connectionRepo *repository.ConnectionRepository
	encryptor      *utils.Encryptor
	shopeeConfig   *ShopeeProviderConfig
	tiktokConfig   *TikTokProviderConfig
	logger         *zap.Logger
}

// ShopeeProviderConfig holds Shopee configuration.
type ShopeeProviderConfig struct {
	PartnerID      string
	PartnerKey     string
	RedirectURL    string
	WebhookURL     string
	IsSandbox      bool
	RequestTimeout time.Duration
}

// TikTokProviderConfig holds TikTok configuration.
type TikTokProviderConfig struct {
	AppKey      string
	AppSecret   string
	RedirectURL string
}

// ProviderFactoryConfig holds configuration for the factory service.
type ProviderFactoryConfig struct {
	EncryptionKey string
	Shopee        *ShopeeProviderConfig
	TikTok        *TikTokProviderConfig
}

// NewProviderFactoryService creates a new provider factory service.
func NewProviderFactoryService(
	connectionRepo *repository.ConnectionRepository,
	cfg *ProviderFactoryConfig,
	logger *zap.Logger,
) (*ProviderFactoryService, error) {
	var encryptor *utils.Encryptor
	var err error

	if cfg.EncryptionKey != "" {
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create encryptor: %w", err)
		}
	}

	return &ProviderFactoryService{
		connectionRepo: connectionRepo,
		encryptor:      encryptor,
		shopeeConfig:   cfg.Shopee,
		tiktokConfig:   cfg.TikTok,
		logger:         logger,
	}, nil
}

// CreateShopeeProvider creates an unauthenticated Shopee provider.
func (f *ProviderFactoryService) CreateShopeeProvider() (*shopee.Provider, error) {
	if f.shopeeConfig == nil {
		return nil, fmt.Errorf("Shopee configuration not provided")
	}

	return shopee.NewProvider(&shopee.ProviderConfig{
		PartnerID:      f.shopeeConfig.PartnerID,
		PartnerKey:     f.shopeeConfig.PartnerKey,
		RedirectURL:    f.shopeeConfig.RedirectURL,
		WebhookURL:     f.shopeeConfig.WebhookURL,
		IsSandbox:      f.shopeeConfig.IsSandbox,
		RequestTimeout: f.shopeeConfig.RequestTimeout,
	}, f.logger)
}

// CreateShopeeProviderForConnection creates a Shopee provider configured for a connection.
func (f *ProviderFactoryService) CreateShopeeProviderForConnection(ctx context.Context, connectionID uuid.UUID) (*shopee.Provider, error) {
	conn, err := f.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	if conn.Platform != "shopee" {
		return nil, fmt.Errorf("connection is not a Shopee connection")
	}

	return f.createShopeeProviderFromConnection(ctx, conn)
}

// CreateProviderForConnection creates a provider for a connection by ID.
// Note: Only Shopee currently has a full MarketplaceProvider implementation.
// For TikTok, use CreateTikTokProviderForConnection instead.
func (f *ProviderFactoryService) CreateProviderForConnection(ctx context.Context, connectionID uuid.UUID) (providers.MarketplaceProvider, error) {
	conn, err := f.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	switch conn.Platform {
	case "shopee":
		return f.createShopeeProviderFromConnection(ctx, conn)
	case "tiktok":
		return nil, fmt.Errorf("TikTok does not implement full MarketplaceProvider interface; use CreateTikTokProviderForConnection")
	default:
		return nil, fmt.Errorf("unsupported platform: %s", conn.Platform)
	}
}

// CreateTikTokProviderForConnection creates a TikTok auth provider for a connection.
func (f *ProviderFactoryService) CreateTikTokProviderForConnection(ctx context.Context, connectionID uuid.UUID) (*tiktok.AuthProvider, error) {
	conn, err := f.connectionRepo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	if conn.Platform != "tiktok" {
		return nil, fmt.Errorf("connection is not a TikTok connection")
	}

	return f.createTikTokProviderFromConnection(ctx, conn)
}

// createShopeeProviderFromConnection creates a Shopee provider from a connection model.
func (f *ProviderFactoryService) createShopeeProviderFromConnection(ctx context.Context, conn *models.Connection) (*shopee.Provider, error) {
	if f.shopeeConfig == nil {
		return nil, fmt.Errorf("Shopee configuration not provided")
	}

	provider, err := shopee.NewProvider(&shopee.ProviderConfig{
		PartnerID:      f.shopeeConfig.PartnerID,
		PartnerKey:     f.shopeeConfig.PartnerKey,
		RedirectURL:    f.shopeeConfig.RedirectURL,
		WebhookURL:     f.shopeeConfig.WebhookURL,
		IsSandbox:      f.shopeeConfig.IsSandbox,
		RequestTimeout: f.shopeeConfig.RequestTimeout,
	}, f.logger)
	if err != nil {
		return nil, err
	}

	// Decrypt tokens
	accessToken, refreshToken, err := f.decryptTokens(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tokens: %w", err)
	}

	// Parse shop ID
	shopID, err := strconv.ParseInt(conn.ShopID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid shop ID: %w", err)
	}

	// Set credentials with refresh capability
	var expiresAt time.Time
	if conn.TokenExpiresAt != nil {
		expiresAt = *conn.TokenExpiresAt
	}

	// Create a token refresher that persists to database
	refresher := &connectionTokenRefresher{
		factory:      f,
		connectionID: conn.ID,
	}

	provider.SetCredentialsWithRefresh(accessToken, refreshToken, shopID, expiresAt, refresher)

	return provider, nil
}

// createTikTokProviderFromConnection creates a TikTok auth provider from a connection model.
// Note: TikTok provider implements a subset of MarketplaceProvider interface.
func (f *ProviderFactoryService) createTikTokProviderFromConnection(ctx context.Context, conn *models.Connection) (*tiktok.AuthProvider, error) {
	if f.tiktokConfig == nil {
		return nil, fmt.Errorf("TikTok configuration not provided")
	}

	client := tiktok.NewClient(&tiktok.ClientConfig{
		AppKey:      f.tiktokConfig.AppKey,
		AppSecret:   f.tiktokConfig.AppSecret,
		RedirectURL: f.tiktokConfig.RedirectURL,
		Logger:      f.logger,
	})

	// Decrypt tokens
	accessToken, _, err := f.decryptTokens(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tokens: %w", err)
	}

	client.SetTokens(accessToken, conn.ShopID)

	return tiktok.NewAuthProvider(client, f.tiktokConfig.RedirectURL), nil
}

// decryptTokens decrypts the access and refresh tokens from a connection.
func (f *ProviderFactoryService) decryptTokens(conn *models.Connection) (accessToken, refreshToken string, err error) {
	accessToken = conn.AccessToken
	refreshToken = conn.RefreshToken

	if f.encryptor == nil {
		return accessToken, refreshToken, nil
	}

	if accessToken != "" {
		accessToken, err = f.encryptor.Decrypt(accessToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to decrypt access token: %w", err)
		}
	}

	if refreshToken != "" {
		refreshToken, err = f.encryptor.Decrypt(refreshToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to decrypt refresh token: %w", err)
		}
	}

	return accessToken, refreshToken, nil
}

// encryptTokens encrypts access and refresh tokens.
func (f *ProviderFactoryService) encryptTokens(accessToken, refreshToken string) (encAccessToken, encRefreshToken string, err error) {
	if f.encryptor == nil {
		return accessToken, refreshToken, nil
	}

	if accessToken != "" {
		encAccessToken, err = f.encryptor.Encrypt(accessToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to encrypt access token: %w", err)
		}
	}

	if refreshToken != "" {
		encRefreshToken, err = f.encryptor.Encrypt(refreshToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to encrypt refresh token: %w", err)
		}
	}

	return encAccessToken, encRefreshToken, nil
}

// connectionTokenRefresher implements shopee.TokenRefresher for a specific connection.
type connectionTokenRefresher struct {
	factory      *ProviderFactoryService
	connectionID uuid.UUID
}

// RefreshToken refreshes the token and persists to database.
func (r *connectionTokenRefresher) RefreshToken(ctx context.Context, refreshToken string, shopID int64) (*shopee.TokenRefreshResult, error) {
	if r.factory.shopeeConfig == nil {
		return nil, fmt.Errorf("Shopee configuration not provided")
	}

	// Create a temporary client for token refresh
	client, err := shopee.NewClient(&shopee.ClientConfig{
		PartnerID:  r.factory.shopeeConfig.PartnerID,
		PartnerKey: r.factory.shopeeConfig.PartnerKey,
		IsSandbox:  r.factory.shopeeConfig.IsSandbox,
		Logger:     r.factory.logger,
	})
	if err != nil {
		return nil, err
	}

	authProvider := shopee.NewAuthProvider(client, "")
	tokenResp, err := authProvider.RefreshToken(ctx, refreshToken, shopID)
	if err != nil {
		return nil, err
	}

	// Persist to database
	encAccessToken, encRefreshToken, err := r.factory.encryptTokens(tokenResp.AccessToken, tokenResp.RefreshToken)
	if err != nil {
		r.factory.logger.Warn("failed to encrypt refreshed tokens",
			zap.String("connection_id", r.connectionID.String()),
			zap.Error(err),
		)
	} else {
		if updateErr := r.factory.connectionRepo.UpdateTokens(ctx, r.connectionID, encAccessToken, encRefreshToken, tokenResp.ExpiresAt); updateErr != nil {
			r.factory.logger.Warn("failed to persist refreshed tokens",
				zap.String("connection_id", r.connectionID.String()),
				zap.Error(updateErr),
			)
		}
	}

	return &shopee.TokenRefreshResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    int64(time.Until(tokenResp.ExpiresAt).Seconds()),
	}, nil
}

// IsShopeeConfigured returns true if Shopee is configured.
func (f *ProviderFactoryService) IsShopeeConfigured() bool {
	return f.shopeeConfig != nil && f.shopeeConfig.PartnerID != "" && f.shopeeConfig.PartnerKey != ""
}

// IsTikTokConfigured returns true if TikTok is configured.
func (f *ProviderFactoryService) IsTikTokConfigured() bool {
	return f.tiktokConfig != nil && f.tiktokConfig.AppKey != "" && f.tiktokConfig.AppSecret != ""
}
