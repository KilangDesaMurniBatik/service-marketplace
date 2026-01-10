package shopee

import (
	"errors"
	"time"
)

// Token validation errors.
var (
	ErrTokenEmpty      = errors.New("token cannot be empty")
	ErrTokenExpirySoon = errors.New("token expires within buffer period")
)

// DefaultExpiryBuffer is the default buffer before token expiration to trigger refresh.
const DefaultExpiryBuffer = 5 * time.Minute

// Token represents Shopee OAuth credentials.
type Token struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	shopID       int64
}

// NewToken creates a new Token value object.
func NewToken(accessToken, refreshToken string, expiresAt time.Time, shopID int64) (*Token, error) {
	if accessToken == "" {
		return nil, ErrTokenEmpty
	}

	return &Token{
		accessToken:  accessToken,
		refreshToken: refreshToken,
		expiresAt:    expiresAt,
		shopID:       shopID,
	}, nil
}

// AccessToken returns the access token string.
func (t *Token) AccessToken() string {
	return t.accessToken
}

// RefreshToken returns the refresh token string.
func (t *Token) RefreshToken() string {
	return t.refreshToken
}

// ExpiresAt returns the expiration time.
func (t *Token) ExpiresAt() time.Time {
	return t.expiresAt
}

// ShopID returns the associated shop ID.
func (t *Token) ShopID() int64 {
	return t.shopID
}

// IsExpired returns true if the token has expired.
func (t *Token) IsExpired() bool {
	return time.Now().After(t.expiresAt)
}

// IsExpiredWithBuffer returns true if the token will expire within the buffer period.
func (t *Token) IsExpiredWithBuffer(buffer time.Duration) bool {
	return time.Now().Add(buffer).After(t.expiresAt)
}

// NeedsRefresh returns true if the token should be refreshed.
func (t *Token) NeedsRefresh() bool {
	return t.IsExpiredWithBuffer(DefaultExpiryBuffer)
}

// TimeUntilExpiry returns the duration until the token expires.
func (t *Token) TimeUntilExpiry() time.Duration {
	return time.Until(t.expiresAt)
}

// WithNewTokens creates a new Token with updated credentials.
func (t *Token) WithNewTokens(accessToken, refreshToken string, expiresAt time.Time) (*Token, error) {
	return NewToken(accessToken, refreshToken, expiresAt, t.shopID)
}

// Validate checks if the token is valid for use.
func (t *Token) Validate() error {
	if t.accessToken == "" {
		return ErrTokenEmpty
	}
	if t.IsExpired() {
		return ErrTokenExpired
	}
	return nil
}
