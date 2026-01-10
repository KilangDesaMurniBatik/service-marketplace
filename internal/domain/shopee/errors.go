// Package shopee provides domain types for the Shopee marketplace integration.
package shopee

import (
	"errors"
	"fmt"
	"net/http"
)

// Standard domain errors.
var (
	ErrTokenExpired       = errors.New("access token has expired")
	ErrRefreshTokenExpired = errors.New("refresh token has expired")
	ErrRateLimited        = errors.New("API rate limit exceeded")
	ErrInvalidSignature   = errors.New("invalid request signature")
	ErrUnauthorized       = errors.New("unauthorized access")
	ErrResourceNotFound   = errors.New("resource not found")
	ErrInvalidRequest     = errors.New("invalid request parameters")
	ErrServiceUnavailable = errors.New("Shopee service temporarily unavailable")
)

// ErrorCode represents Shopee API error codes.
type ErrorCode string

// Shopee API error codes per official documentation.
const (
	// Authentication errors
	CodeAuthError           ErrorCode = "error_auth"
	CodeInvalidSign         ErrorCode = "error_sign"
	CodeInvalidTimestamp    ErrorCode = "error_timestamp"
	CodePermissionDenied    ErrorCode = "error_permission"

	// Token errors
	CodeInvalidParam        ErrorCode = "error_param"

	// Rate limiting
	CodeExceedLimit         ErrorCode = "error_exceed_limit"

	// Server errors
	CodeServerError         ErrorCode = "error_server"

	// Resource errors
	CodeNotFound            ErrorCode = "error_not_found"
	CodeProductBanned       ErrorCode = "error_product_banned"
	CodeOrderCancelled      ErrorCode = "error_order_cancelled"
)

// String returns the string representation of the error code.
func (c ErrorCode) String() string {
	return string(c)
}

// IsRetryable returns true if the error code indicates a retryable error.
func (c ErrorCode) IsRetryable() bool {
	switch c {
	case CodeExceedLimit, CodeServerError:
		return true
	default:
		return false
	}
}

// IsTokenError returns true if the error indicates a token issue.
func (c ErrorCode) IsTokenError() bool {
	return c == CodeAuthError
}

// APIError represents a structured error from the Shopee API.
type APIError struct {
	Code       ErrorCode `json:"error"`
	Message    string    `json:"message"`
	RequestID  string    `json:"request_id,omitempty"`
	StatusCode int       `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("shopee [%s]: %s (request_id: %s)", e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("shopee [%s]: %s", e.Code, e.Message)
}

// Is implements errors.Is for APIError.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrTokenExpired:
		return e.isTokenExpired()
	case ErrRefreshTokenExpired:
		return e.isRefreshTokenExpired()
	case ErrRateLimited:
		return e.Code == CodeExceedLimit || e.StatusCode == http.StatusTooManyRequests
	case ErrInvalidSignature:
		return e.Code == CodeInvalidSign
	case ErrUnauthorized:
		return e.Code == CodePermissionDenied || e.StatusCode == http.StatusUnauthorized
	case ErrResourceNotFound:
		return e.Code == CodeNotFound || e.StatusCode == http.StatusNotFound
	case ErrInvalidRequest:
		return e.Code == CodeInvalidParam || e.StatusCode == http.StatusBadRequest
	case ErrServiceUnavailable:
		return e.Code == CodeServerError || e.StatusCode >= 500
	default:
		return false
	}
}

// IsRetryable returns true if this error is safe to retry.
func (e *APIError) IsRetryable() bool {
	if e.Code.IsRetryable() {
		return true
	}
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusServiceUnavailable ||
		e.StatusCode >= 500
}

// isTokenExpired checks if the error indicates token expiration.
func (e *APIError) isTokenExpired() bool {
	if e.Code != CodeAuthError {
		return false
	}
	// Check message for specific token expiration indicators
	return containsAny(e.Message, "token expired", "invalid token", "access_token invalid")
}

// isRefreshTokenExpired checks if the refresh token has expired.
func (e *APIError) isRefreshTokenExpired() bool {
	return e.Code == CodeInvalidParam &&
		containsAny(e.Message, "refresh_token", "invalid refresh")
}

// NewAPIError creates a new APIError with the given parameters.
func NewAPIError(code ErrorCode, message string, statusCode int) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

// NewAPIErrorWithRequestID creates a new APIError with request ID.
func NewAPIErrorWithRequestID(code ErrorCode, message string, statusCode int, requestID string) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
		RequestID:  requestID,
	}
}

// containsAny checks if the string contains any of the substrings.
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// ErrorCategory classifies errors into categories.
type ErrorCategory string

const (
	CategoryAuthentication ErrorCategory = "authentication"
	CategoryRateLimit      ErrorCategory = "rate_limit"
	CategoryServer         ErrorCategory = "server"
	CategoryNotFound       ErrorCategory = "not_found"
	CategoryValidation     ErrorCategory = "validation"
	CategoryUnknown        ErrorCategory = "unknown"
)

// Category returns the category of this error.
func (e *APIError) Category() ErrorCategory {
	switch e.Code {
	case CodeAuthError, CodeInvalidSign, CodeInvalidTimestamp, CodePermissionDenied:
		return CategoryAuthentication
	case CodeExceedLimit:
		return CategoryRateLimit
	case CodeServerError:
		return CategoryServer
	case CodeNotFound, CodeProductBanned, CodeOrderCancelled:
		return CategoryNotFound
	case CodeInvalidParam:
		return CategoryValidation
	default:
		return CategoryUnknown
	}
}
