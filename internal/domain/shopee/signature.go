package shopee

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Signature provides HMAC-SHA256 signature generation for Shopee API.
type Signature struct {
	partnerKey string
}

// NewSignature creates a new Signature utility.
func NewSignature(partnerKey string) *Signature {
	return &Signature{partnerKey: partnerKey}
}

// GeneratePublic generates signature for public (unauthenticated) endpoints.
// Base string: partner_id + api_path + timestamp
func (s *Signature) GeneratePublic(partnerID int64, path string, timestamp int64) string {
	baseString := fmt.Sprintf("%d%s%d", partnerID, path, timestamp)
	return s.sign(baseString)
}

// GenerateAuthenticated generates signature for authenticated shop endpoints.
// Base string: partner_id + api_path + timestamp + access_token + shop_id
func (s *Signature) GenerateAuthenticated(partnerID int64, path string, timestamp int64, accessToken string, shopID int64) string {
	baseString := fmt.Sprintf("%d%s%d%s%d", partnerID, path, timestamp, accessToken, shopID)
	return s.sign(baseString)
}

// GenerateMerchant generates signature for merchant-level endpoints.
// Base string: partner_id + api_path + timestamp + access_token + merchant_id
func (s *Signature) GenerateMerchant(partnerID int64, path string, timestamp int64, accessToken string, merchantID int64) string {
	baseString := fmt.Sprintf("%d%s%d%s%d", partnerID, path, timestamp, accessToken, merchantID)
	return s.sign(baseString)
}

// VerifyWebhook verifies the signature of an incoming webhook request.
// Shopee webhook signature: HMAC-SHA256(base_url + "|" + body)
func (s *Signature) VerifyWebhook(baseURL string, body []byte, providedSignature string) bool {
	baseString := baseURL + "|" + string(body)
	expectedSignature := s.sign(baseString)
	return hmac.Equal([]byte(expectedSignature), []byte(providedSignature))
}

// sign computes the HMAC-SHA256 signature.
func (s *Signature) sign(baseString string) string {
	h := hmac.New(sha256.New, []byte(s.partnerKey))
	h.Write([]byte(baseString))
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateTimestamp checks if the timestamp is within acceptable range.
// Shopee allows a 5-minute window for timestamp validation.
func ValidateTimestamp(timestamp, serverTimestamp int64) bool {
	const maxDrift = 300 // 5 minutes in seconds
	diff := serverTimestamp - timestamp
	if diff < 0 {
		diff = -diff
	}
	return diff <= maxDrift
}
