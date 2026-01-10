package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/providers/shopee"
	"github.com/niaga-platform/service-marketplace/internal/repository"
	"github.com/niaga-platform/service-marketplace/internal/utils"
)

// TokenManagerConfig holds configuration for the token manager.
type TokenManagerConfig struct {
	RefreshBuffer   time.Duration // How long before expiry to trigger refresh
	CheckInterval   time.Duration // How often to check for expiring tokens
	EncryptionKey   string
	ShopeePartnerID string
	ShopeePartnerKey string
	ShopeeSandbox   bool
}

// TokenManager handles automatic token refresh for marketplace connections.
type TokenManager struct {
	repo      *repository.ConnectionRepository
	encryptor *utils.Encryptor
	config    TokenManagerConfig
	logger    *zap.Logger

	// Shopee client for token refresh
	shopeeClient *shopee.Client

	// Lifecycle management
	stopChan chan struct{}
	wg       sync.WaitGroup
	running  bool
	mu       sync.Mutex
}

// NewTokenManager creates a new token manager service.
func NewTokenManager(
	repo *repository.ConnectionRepository,
	cfg TokenManagerConfig,
	logger *zap.Logger,
) (*TokenManager, error) {
	var encryptor *utils.Encryptor
	var err error

	if cfg.EncryptionKey != "" {
		encryptor, err = utils.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create encryptor: %w", err)
		}
	}

	// Set defaults
	if cfg.RefreshBuffer == 0 {
		cfg.RefreshBuffer = 10 * time.Minute
	}
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 5 * time.Minute
	}

	// Create Shopee client for token refresh
	var shopeeClient *shopee.Client
	if cfg.ShopeePartnerID != "" && cfg.ShopeePartnerKey != "" {
		shopeeClient, err = shopee.NewClient(&shopee.ClientConfig{
			PartnerID:  cfg.ShopeePartnerID,
			PartnerKey: cfg.ShopeePartnerKey,
			IsSandbox:  cfg.ShopeeSandbox,
			Logger:     logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Shopee client: %w", err)
		}
	}

	return &TokenManager{
		repo:         repo,
		encryptor:    encryptor,
		config:       cfg,
		logger:       logger,
		shopeeClient: shopeeClient,
		stopChan:     make(chan struct{}),
	}, nil
}

// Start begins the background token refresh process.
func (tm *TokenManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	if tm.running {
		tm.mu.Unlock()
		return fmt.Errorf("token manager already running")
	}
	tm.running = true
	tm.mu.Unlock()

	tm.wg.Add(1)
	go tm.run(ctx)

	tm.logger.Info("token manager started",
		zap.Duration("check_interval", tm.config.CheckInterval),
		zap.Duration("refresh_buffer", tm.config.RefreshBuffer),
	)

	return nil
}

// Stop gracefully stops the token manager.
func (tm *TokenManager) Stop() {
	tm.mu.Lock()
	if !tm.running {
		tm.mu.Unlock()
		return
	}
	tm.running = false
	tm.mu.Unlock()

	close(tm.stopChan)
	tm.wg.Wait()

	tm.logger.Info("token manager stopped")
}

// run is the main background loop.
func (tm *TokenManager) run(ctx context.Context) {
	defer tm.wg.Done()

	ticker := time.NewTicker(tm.config.CheckInterval)
	defer ticker.Stop()

	// Do an initial check
	tm.checkAndRefreshTokens(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-tm.stopChan:
			return
		case <-ticker.C:
			tm.checkAndRefreshTokens(ctx)
		}
	}
}

// checkAndRefreshTokens checks for expiring tokens and refreshes them.
func (tm *TokenManager) checkAndRefreshTokens(ctx context.Context) {
	// Calculate the threshold time (tokens expiring within the buffer period)
	bufferMinutes := int(tm.config.RefreshBuffer.Minutes())

	connections, err := tm.repo.GetConnectionsNeedingTokenRefresh(ctx, bufferMinutes)
	if err != nil {
		tm.logger.Error("failed to get connections needing refresh", zap.Error(err))
		return
	}

	if len(connections) == 0 {
		return
	}

	tm.logger.Info("found connections needing token refresh",
		zap.Int("count", len(connections)),
	)

	for _, conn := range connections {
		if err := tm.refreshConnection(ctx, &conn); err != nil {
			tm.logger.Error("failed to refresh connection token",
				zap.String("connection_id", conn.ID.String()),
				zap.String("platform", conn.Platform),
				zap.String("shop_id", conn.ShopID),
				zap.Error(err),
			)
			continue
		}

		tm.logger.Info("successfully refreshed token",
			zap.String("connection_id", conn.ID.String()),
			zap.String("platform", conn.Platform),
			zap.String("shop_id", conn.ShopID),
		)
	}
}

// refreshConnection refreshes the token for a single connection.
func (tm *TokenManager) refreshConnection(ctx context.Context, conn *models.Connection) error {
	// Decrypt refresh token
	refreshToken := conn.RefreshToken
	if tm.encryptor != nil && refreshToken != "" {
		decrypted, err := tm.encryptor.Decrypt(refreshToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt refresh token: %w", err)
		}
		refreshToken = decrypted
	}

	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	var newAccessToken, newRefreshToken string
	var expiresAt time.Time

	switch conn.Platform {
	case "shopee":
		result, err := tm.refreshShopeeToken(ctx, refreshToken, conn.ShopID)
		if err != nil {
			return err
		}
		newAccessToken = result.AccessToken
		newRefreshToken = result.RefreshToken
		expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

	default:
		return fmt.Errorf("unsupported platform: %s", conn.Platform)
	}

	// Encrypt new tokens
	if tm.encryptor != nil {
		var err error
		newAccessToken, err = tm.encryptor.Encrypt(newAccessToken)
		if err != nil {
			return fmt.Errorf("failed to encrypt access token: %w", err)
		}
		newRefreshToken, err = tm.encryptor.Encrypt(newRefreshToken)
		if err != nil {
			return fmt.Errorf("failed to encrypt refresh token: %w", err)
		}
	}

	// Update in database
	return tm.repo.UpdateTokens(ctx, conn.ID, newAccessToken, newRefreshToken, expiresAt)
}

// refreshShopeeToken refreshes a Shopee access token.
func (tm *TokenManager) refreshShopeeToken(ctx context.Context, refreshToken, shopIDStr string) (*shopee.TokenRefreshResult, error) {
	if tm.shopeeClient == nil {
		return nil, fmt.Errorf("Shopee client not configured")
	}

	var shopID int64
	fmt.Sscanf(shopIDStr, "%d", &shopID)

	// Create auth provider for token refresh
	authProvider := shopee.NewAuthProvider(tm.shopeeClient, "")

	tokenResp, err := authProvider.RefreshToken(ctx, refreshToken, shopID)
	if err != nil {
		return nil, err
	}

	return &shopee.TokenRefreshResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    int64(time.Until(tokenResp.ExpiresAt).Seconds()),
	}, nil
}

// RefreshTokenForConnection immediately refreshes the token for a specific connection.
// This can be called manually or by the client when token expiration is detected.
func (tm *TokenManager) RefreshTokenForConnection(ctx context.Context, connectionID uuid.UUID) error {
	conn, err := tm.repo.GetByID(ctx, connectionID)
	if err != nil {
		return fmt.Errorf("connection not found: %w", err)
	}

	return tm.refreshConnection(ctx, conn)
}

// GetDecryptedTokens retrieves and decrypts tokens for a connection.
func (tm *TokenManager) GetDecryptedTokens(ctx context.Context, connectionID uuid.UUID) (accessToken, refreshToken string, err error) {
	conn, err := tm.repo.GetByID(ctx, connectionID)
	if err != nil {
		return "", "", fmt.Errorf("connection not found: %w", err)
	}

	accessToken = conn.AccessToken
	refreshToken = conn.RefreshToken

	if tm.encryptor != nil {
		if accessToken != "" {
			accessToken, err = tm.encryptor.Decrypt(accessToken)
			if err != nil {
				return "", "", fmt.Errorf("failed to decrypt access token: %w", err)
			}
		}
		if refreshToken != "" {
			refreshToken, err = tm.encryptor.Decrypt(refreshToken)
			if err != nil {
				return "", "", fmt.Errorf("failed to decrypt refresh token: %w", err)
			}
		}
	}

	return accessToken, refreshToken, nil
}

// TokenRefresherAdapter adapts TokenManager to the shopee.TokenRefresher interface.
type TokenRefresherAdapter struct {
	manager      *TokenManager
	connectionID uuid.UUID
}

// NewTokenRefresherAdapter creates an adapter for a specific connection.
func NewTokenRefresherAdapter(manager *TokenManager, connectionID uuid.UUID) *TokenRefresherAdapter {
	return &TokenRefresherAdapter{
		manager:      manager,
		connectionID: connectionID,
	}
}

// RefreshToken implements the shopee.TokenRefresher interface.
func (a *TokenRefresherAdapter) RefreshToken(ctx context.Context, refreshToken string, shopID int64) (*shopee.TokenRefreshResult, error) {
	// First try to refresh using the provided refresh token
	result, err := a.manager.refreshShopeeToken(ctx, refreshToken, fmt.Sprintf("%d", shopID))
	if err != nil {
		return nil, err
	}

	// Update the connection in the database
	if err := a.updateConnectionTokens(ctx, result); err != nil {
		a.manager.logger.Warn("failed to persist refreshed tokens",
			zap.String("connection_id", a.connectionID.String()),
			zap.Error(err),
		)
	}

	return result, nil
}

// updateConnectionTokens persists the new tokens to the database.
func (a *TokenRefresherAdapter) updateConnectionTokens(ctx context.Context, result *shopee.TokenRefreshResult) error {
	accessToken := result.AccessToken
	refreshToken := result.RefreshToken

	if a.manager.encryptor != nil {
		var err error
		accessToken, err = a.manager.encryptor.Encrypt(accessToken)
		if err != nil {
			return err
		}
		refreshToken, err = a.manager.encryptor.Encrypt(refreshToken)
		if err != nil {
			return err
		}
	}

	expiresAt := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return a.manager.repo.UpdateTokens(ctx, a.connectionID, accessToken, refreshToken, expiresAt)
}
