package shopee

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	shopeedomain "github.com/niaga-platform/service-marketplace/internal/domain/shopee"
)

const (
	ProductionBaseURL = "https://partner.shopeemobile.com"
	SandboxBaseURL    = "https://openplatform.sandbox.test-stable.shopee.sg"
)

// TokenRefresher defines the interface for refreshing tokens.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, refreshToken string, shopID int64) (*TokenRefreshResult, error)
}

// TokenRefreshResult holds the result of a token refresh operation.
type TokenRefreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

// Client is the production-grade Shopee API client with automatic retry,
// token refresh, and rate limiting support.
type Client struct {
	partnerID    int64
	partnerKey   string
	baseURL      string
	httpClient   *http.Client
	logger       *zap.Logger
	retryPolicy  *shopeedomain.RetryPolicy
	signature    *shopeedomain.Signature

	// Token management with thread safety
	tokenMu      sync.RWMutex
	accessToken  string
	refreshToken string
	shopID       int64
	tokenExpiry  time.Time

	// Token refresher callback for automatic refresh
	tokenRefresher TokenRefresher
}

// ClientConfig holds configuration for the Shopee client.
type ClientConfig struct {
	PartnerID      string
	PartnerKey     string
	IsSandbox      bool
	RedirectURL    string
	Logger         *zap.Logger
	RetryPolicy    *shopeedomain.RetryPolicy
	RequestTimeout time.Duration
}

// NewClient creates a new production-grade Shopee API client.
func NewClient(cfg *ClientConfig) (*Client, error) {
	partnerID, err := strconv.ParseInt(cfg.PartnerID, 10, 64)
	if err != nil && cfg.PartnerID != "" {
		return nil, fmt.Errorf("invalid partner ID: %w", err)
	}

	baseURL := ProductionBaseURL
	if cfg.IsSandbox {
		baseURL = SandboxBaseURL
	}

	timeout := cfg.RequestTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	retryPolicy := cfg.RetryPolicy
	if retryPolicy == nil {
		retryPolicy = shopeedomain.DefaultRetryPolicy()
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Client{
		partnerID:   partnerID,
		partnerKey:  cfg.PartnerKey,
		baseURL:     baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger:      logger,
		retryPolicy: retryPolicy,
		signature:   shopeedomain.NewSignature(cfg.PartnerKey),
	}, nil
}

// SetTokens sets the access token and shop ID for authenticated requests.
func (c *Client) SetTokens(accessToken string, shopID int64) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.accessToken = accessToken
	c.shopID = shopID
}

// SetTokensWithRefresh sets tokens with refresh capability.
func (c *Client) SetTokensWithRefresh(accessToken, refreshToken string, shopID int64, expiresAt time.Time) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.accessToken = accessToken
	c.refreshToken = refreshToken
	c.shopID = shopID
	c.tokenExpiry = expiresAt
}

// SetTokenRefresher sets the callback for automatic token refresh.
func (c *Client) SetTokenRefresher(refresher TokenRefresher) {
	c.tokenRefresher = refresher
}

// GetPartnerID returns the partner ID.
func (c *Client) GetPartnerID() int64 {
	return c.partnerID
}

// GetShopID returns the current shop ID.
func (c *Client) GetShopID() int64 {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.shopID
}

// generateSign generates the HMAC-SHA256 signature for Shopee API.
func (c *Client) generateSign(path string, timestamp int64) string {
	return c.signature.GeneratePublic(c.partnerID, path, timestamp)
}

// generateSignWithTokens generates signature for authenticated endpoints.
func (c *Client) generateSignWithTokens(path string, timestamp int64, accessToken string, shopID int64) string {
	return c.signature.GenerateAuthenticated(c.partnerID, path, timestamp, accessToken, shopID)
}

// Request represents a generic API request.
type Request struct {
	Method   string
	Path     string
	Query    map[string]string
	Body     interface{}
	NeedAuth bool
}

// Do performs an HTTP request to the Shopee API with automatic retry and token refresh.
func (c *Client) Do(ctx context.Context, req *Request, result interface{}) error {
	executor := shopeedomain.NewExecutor(c.retryPolicy)

	retryResult := executor.Execute(ctx, func() error {
		err := c.doRequest(ctx, req, result)
		if err != nil {
			// Handle token expiration with automatic refresh
			if shopeedomain.ErrTokenExpired.Error() == err.Error() || isTokenExpiredError(err) {
				if refreshErr := c.tryRefreshToken(ctx); refreshErr != nil {
					c.logger.Warn("failed to refresh token",
						zap.Error(refreshErr),
						zap.String("path", req.Path),
					)
					return err
				}
				// Token refreshed, this error is now retryable
				return shopeedomain.NewAPIError(shopeedomain.CodeAuthError, "token refreshed, retrying", http.StatusUnauthorized)
			}
		}
		return err
	})

	if retryResult.LastError != nil {
		c.logger.Error("Shopee API request failed after retries",
			zap.String("path", req.Path),
			zap.Int("attempts", retryResult.Attempts),
			zap.Duration("duration", retryResult.Duration),
			zap.Error(retryResult.LastError),
		)
		return retryResult.LastError
	}

	return nil
}

// doRequest performs a single HTTP request without retry.
func (c *Client) doRequest(ctx context.Context, req *Request, result interface{}) error {
	timestamp := time.Now().Unix()

	// Get current tokens
	c.tokenMu.RLock()
	accessToken := c.accessToken
	shopID := c.shopID
	c.tokenMu.RUnlock()

	// Build URL with common params
	url := c.baseURL + req.Path
	queryParams := []string{
		fmt.Sprintf("partner_id=%d", c.partnerID),
		fmt.Sprintf("timestamp=%d", timestamp),
	}

	var sign string
	if req.NeedAuth {
		sign = c.generateSignWithTokens(req.Path, timestamp, accessToken, shopID)
		queryParams = append(queryParams, fmt.Sprintf("access_token=%s", accessToken))
		queryParams = append(queryParams, fmt.Sprintf("shop_id=%d", shopID))
	} else {
		sign = c.generateSign(req.Path, timestamp)
	}
	queryParams = append(queryParams, fmt.Sprintf("sign=%s", sign))

	// Add custom query params
	for k, v := range req.Query {
		queryParams = append(queryParams, fmt.Sprintf("%s=%s", k, v))
	}

	// Sort query params for consistency
	sort.Strings(queryParams)
	url += "?" + strings.Join(queryParams, "&")

	// Build request body
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	startTime := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Log request/response
	c.logger.Debug("Shopee API request completed",
		zap.String("method", req.Method),
		zap.String("path", req.Path),
		zap.Int("status", resp.StatusCode),
		zap.Duration("latency", time.Since(startTime)),
		zap.String("response", truncateString(string(respBody), 500)),
	)

	// Parse base response to check for errors
	var baseResp struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(respBody, &baseResp); err == nil {
		if baseResp.Error != "" && baseResp.Error != "success" {
			apiErr := shopeedomain.NewAPIErrorWithRequestID(
				shopeedomain.ErrorCode(baseResp.Error),
				baseResp.Message,
				resp.StatusCode,
				baseResp.RequestID,
			)

			c.logger.Warn("Shopee API error",
				zap.String("path", req.Path),
				zap.String("error_code", baseResp.Error),
				zap.String("message", baseResp.Message),
				zap.String("request_id", baseResp.RequestID),
			)

			return apiErr
		}
	}

	// Handle HTTP-level errors
	if resp.StatusCode >= 400 {
		return shopeedomain.NewAPIError(
			shopeedomain.CodeServerError,
			fmt.Sprintf("HTTP error: %d", resp.StatusCode),
			resp.StatusCode,
		)
	}

	// Parse response
	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// tryRefreshToken attempts to refresh the access token.
func (c *Client) tryRefreshToken(ctx context.Context) error {
	if c.tokenRefresher == nil {
		return fmt.Errorf("no token refresher configured")
	}

	c.tokenMu.Lock()
	refreshToken := c.refreshToken
	shopID := c.shopID
	c.tokenMu.Unlock()

	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	result, err := c.tokenRefresher.RefreshToken(ctx, refreshToken, shopID)
	if err != nil {
		return err
	}

	// Update tokens
	c.tokenMu.Lock()
	c.accessToken = result.AccessToken
	c.refreshToken = result.RefreshToken
	c.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	c.tokenMu.Unlock()

	c.logger.Info("token refreshed successfully",
		zap.Int64("shop_id", shopID),
		zap.Time("new_expiry", c.tokenExpiry),
	)

	return nil
}

// isTokenExpiredError checks if an error indicates token expiration.
func isTokenExpiredError(err error) bool {
	if apiErr, ok := err.(*shopeedomain.APIError); ok {
		return apiErr.Code == shopeedomain.CodeAuthError
	}
	return false
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// BaseResponse is the common response structure from Shopee API.
type BaseResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Warning   string `json:"warning,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// HasError checks if the response contains an error.
func (r *BaseResponse) HasError() bool {
	return r.Error != "" && r.Error != "success"
}

// GetError returns the error message.
func (r *BaseResponse) GetError() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Error
}

// ToAPIError converts BaseResponse to APIError.
func (r *BaseResponse) ToAPIError(statusCode int) *shopeedomain.APIError {
	return shopeedomain.NewAPIErrorWithRequestID(
		shopeedomain.ErrorCode(r.Error),
		r.Message,
		statusCode,
		r.RequestID,
	)
}
