package shopee

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	shopeedomain "github.com/niaga-platform/service-marketplace/internal/domain/shopee"
	"github.com/niaga-platform/service-marketplace/internal/providers"
)

// WebhookEventType represents the type of Shopee webhook event.
type WebhookEventType int

// Webhook event type constants.
const (
	WebhookEventUnknown WebhookEventType = iota
	WebhookEventShopAuthorization
	WebhookEventOrderStatusUpdate
	WebhookEventTrackingUpdate
	WebhookEventItemPromotion
	WebhookEventReservedStockChange
	WebhookEventBrandRegister
	WebhookEventOpenApi
	WebhookEventWebhookTest
)

// Webhook push codes from Shopee.
const (
	PushCodeShopAuthorization   = 1
	PushCodeOrderStatusUpdate   = 3
	PushCodeTrackingUpdate      = 4
	PushCodeItemPromotion       = 5
	PushCodeReservedStockChange = 6
	PushCodeBrandRegister       = 7
	PushCodeOpenApi             = 8
	PushCodeWebhookTest         = 10
)

// WebhookPayload represents the raw webhook payload from Shopee.
type WebhookPayload struct {
	Code      int             `json:"code"`
	ShopID    int64           `json:"shop_id"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// ShopAuthorizationData represents shop authorization webhook data.
type ShopAuthorizationData struct {
	Code    int   `json:"code"`
	ShopID  int64 `json:"shop_id"`
	Message string `json:"msg"`
}

// OrderStatusData represents order status update webhook data.
type OrderStatusData struct {
	OrderSN     string `json:"ordersn"`
	Status      string `json:"status"`
	UpdateTime  int64  `json:"update_time"`
	ShopID      int64  `json:"shop_id"`
}

// TrackingUpdateData represents tracking update webhook data.
type TrackingUpdateData struct {
	OrderSN        string `json:"ordersn"`
	TrackingNumber string `json:"tracking_number"`
	ShopID         int64  `json:"shop_id"`
	LogisticsStatus string `json:"logistics_status"`
	UpdateTime     int64  `json:"update_time"`
}

// WebhookHandler handles incoming Shopee webhooks.
type WebhookHandler struct {
	signature  *shopeedomain.Signature
	webhookURL string
	logger     *zap.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(partnerKey, webhookURL string, logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{
		signature:  shopeedomain.NewSignature(partnerKey),
		webhookURL: webhookURL,
		logger:     logger,
	}
}

// VerifyWebhook verifies the signature of an incoming webhook.
func (h *WebhookHandler) VerifyWebhook(ctx context.Context, body []byte, headers map[string]string) (bool, error) {
	// Get the authorization header which contains the signature
	authorization := headers["Authorization"]
	if authorization == "" {
		authorization = headers["authorization"]
	}

	if authorization == "" {
		h.logger.Warn("webhook missing authorization header")
		return false, nil
	}

	// Verify the signature
	isValid := h.signature.VerifyWebhook(h.webhookURL, body, authorization)

	if !isValid {
		h.logger.Warn("webhook signature verification failed",
			zap.String("provided_signature", authorization),
		)
	}

	return isValid, nil
}

// ParseWebhookEvent parses a raw webhook body into a structured event.
func (h *WebhookHandler) ParseWebhookEvent(body []byte) (*providers.WebhookEvent, error) {
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	eventType := h.mapEventType(payload.Code)
	event := &providers.WebhookEvent{
		Type:      eventType,
		ShopID:    fmt.Sprintf("%d", payload.ShopID),
		Timestamp: time.Unix(payload.Timestamp, 0),
	}

	// Parse event-specific data
	switch payload.Code {
	case PushCodeShopAuthorization:
		var data ShopAuthorizationData
		if err := json.Unmarshal(payload.Data, &data); err == nil {
			event.Payload = data
		}

	case PushCodeOrderStatusUpdate:
		var data OrderStatusData
		if err := json.Unmarshal(payload.Data, &data); err == nil {
			event.Payload = data
		}

	case PushCodeTrackingUpdate:
		var data TrackingUpdateData
		if err := json.Unmarshal(payload.Data, &data); err == nil {
			event.Payload = data
		}

	default:
		// Store raw data for unknown event types
		var rawData map[string]interface{}
		json.Unmarshal(payload.Data, &rawData)
		event.Payload = rawData
	}

	h.logger.Debug("parsed webhook event",
		zap.String("type", eventType),
		zap.Int64("shop_id", payload.ShopID),
		zap.Int("code", payload.Code),
	)

	return event, nil
}

// mapEventType maps Shopee push codes to event type strings.
func (h *WebhookHandler) mapEventType(code int) string {
	switch code {
	case PushCodeShopAuthorization:
		return "shop.authorization"
	case PushCodeOrderStatusUpdate:
		return "order.status_changed"
	case PushCodeTrackingUpdate:
		return "order.tracking_update"
	case PushCodeItemPromotion:
		return "item.promotion"
	case PushCodeReservedStockChange:
		return "inventory.reserved_changed"
	case PushCodeBrandRegister:
		return "brand.register"
	case PushCodeOpenApi:
		return "openapi"
	case PushCodeWebhookTest:
		return "webhook.test"
	default:
		return fmt.Sprintf("unknown.%d", code)
	}
}

// ExtractOrderSN extracts the order SN from an order-related webhook event.
func ExtractOrderSN(event *providers.WebhookEvent) (string, bool) {
	switch data := event.Payload.(type) {
	case OrderStatusData:
		return data.OrderSN, true
	case TrackingUpdateData:
		return data.OrderSN, true
	case map[string]interface{}:
		if orderSN, ok := data["ordersn"].(string); ok {
			return orderSN, true
		}
	}
	return "", false
}

// ExtractShopID extracts the shop ID from any webhook event data.
func ExtractShopID(event *providers.WebhookEvent) (int64, bool) {
	switch data := event.Payload.(type) {
	case ShopAuthorizationData:
		return data.ShopID, true
	case OrderStatusData:
		return data.ShopID, true
	case TrackingUpdateData:
		return data.ShopID, true
	case map[string]interface{}:
		if shopID, ok := data["shop_id"].(float64); ok {
			return int64(shopID), true
		}
	}
	return 0, false
}
