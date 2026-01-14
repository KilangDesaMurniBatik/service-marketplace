package shopee

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

const (
	GetReturnListPath      = "/api/v2/returns/get_return_list"
	GetReturnDetailPath    = "/api/v2/returns/get_return_detail"
	ConfirmReturnPath      = "/api/v2/returns/confirm"
	DisputeReturnPath      = "/api/v2/returns/dispute"
)

// ReturnProvider implements return operations for Shopee
type ReturnProvider struct {
	client *Client
}

// NewReturnProvider creates a new Shopee return provider
func NewReturnProvider(client *Client) *ReturnProvider {
	return &ReturnProvider{client: client}
}

// ShopeeReturn represents a return from Shopee API
type ShopeeReturn struct {
	ReturnSN        string  `json:"return_sn"`
	OrderSN         string  `json:"order_sn"`
	Reason          string  `json:"reason"`
	TextReason      string  `json:"text_reason"`
	Status          string  `json:"status"`
	RefundAmount    float64 `json:"refund_amount"`
	Currency        string  `json:"currency"`
	CreateTime      int64   `json:"create_time"`
	UpdateTime      int64   `json:"update_time"`
	NeedsLogistics  bool    `json:"needs_logistics"`
	ReturnShipDue   int64   `json:"return_ship_due_date"`
	TrackingNumber  string  `json:"tracking_number"`
	User            ShopeeReturnUser `json:"user"`
	Items           []ShopeeReturnItem `json:"item"`
}

// ShopeeReturnUser represents buyer info in return
type ShopeeReturnUser struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

// ShopeeReturnItem represents an item in a return
type ShopeeReturnItem struct {
	ItemID      int64   `json:"item_id"`
	ModelID     int64   `json:"model_id"`
	Name        string  `json:"name"`
	Images      []string `json:"images"`
	Amount      int     `json:"amount"`
	ItemPrice   float64 `json:"item_price"`
	IsAddOnDeal bool    `json:"is_add_on_deal"`
}

// GetReturns fetches return list from Shopee
func (p *ReturnProvider) GetReturns(ctx context.Context, params *providers.ReturnListParams) ([]providers.ExternalReturn, string, error) {
	query := map[string]string{
		"page_size": fmt.Sprintf("%d", params.PageSize),
	}

	if params.PageSize == 0 {
		query["page_size"] = "50"
	}

	if params.PageNo > 0 {
		query["page_no"] = fmt.Sprintf("%d", params.PageNo)
	}

	// Time filter (optional)
	if !params.CreateTimeFrom.IsZero() {
		query["create_time_from"] = fmt.Sprintf("%d", params.CreateTimeFrom.Unix())
	}
	if !params.CreateTimeTo.IsZero() {
		query["create_time_to"] = fmt.Sprintf("%d", params.CreateTimeTo.Unix())
	}

	req := &Request{
		Method:   http.MethodGet,
		Path:     GetReturnListPath,
		Query:    query,
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			More       bool   `json:"more"`
			ReturnList []struct {
				ReturnSN     string `json:"return_sn"`
				ReturnStatus string `json:"status"`
			} `json:"return"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, "", fmt.Errorf("failed to get returns: %w", err)
	}

	if resp.HasError() {
		return nil, "", fmt.Errorf("shopee error: %s", resp.GetError())
	}

	if len(resp.Response.ReturnList) == 0 {
		return []providers.ExternalReturn{}, "", nil
	}

	// Fetch return details
	returnSNs := make([]string, len(resp.Response.ReturnList))
	for i, r := range resp.Response.ReturnList {
		returnSNs[i] = r.ReturnSN
	}

	returns, err := p.getReturnDetails(ctx, returnSNs)
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if resp.Response.More {
		nextCursor = fmt.Sprintf("%d", params.PageNo+1)
	}

	return returns, nextCursor, nil
}

// getReturnDetails fetches detailed return information
func (p *ReturnProvider) getReturnDetails(ctx context.Context, returnSNs []string) ([]providers.ExternalReturn, error) {
	returns := make([]providers.ExternalReturn, 0, len(returnSNs))

	for _, returnSN := range returnSNs {
		detail, err := p.GetReturn(ctx, returnSN)
		if err != nil {
			// Log but continue with other returns
			continue
		}
		returns = append(returns, *detail)
	}

	return returns, nil
}

// GetReturn fetches a single return detail
func (p *ReturnProvider) GetReturn(ctx context.Context, returnSN string) (*providers.ExternalReturn, error) {
	req := &Request{
		Method: http.MethodGet,
		Path:   GetReturnDetailPath,
		Query: map[string]string{
			"return_sn": returnSN,
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ReturnSN       string  `json:"return_sn"`
			OrderSN        string  `json:"order_sn"`
			Reason         string  `json:"reason"`
			TextReason     string  `json:"text_reason"`
			Status         string  `json:"status"`
			RefundAmount   float64 `json:"refund_amount"`
			Currency       string  `json:"currency"`
			CreateTime     int64   `json:"create_time"`
			UpdateTime     int64   `json:"update_time"`
			NeedsLogistics bool    `json:"needs_logistics"`
			ReturnShipDue  int64   `json:"return_ship_due_date"`
			TrackingNumber string  `json:"tracking_number"`
			User           struct {
				UserID   int64  `json:"user_id"`
				Username string `json:"username"`
			} `json:"user"`
			Item []struct {
				ItemID      int64    `json:"item_id"`
				ModelID     int64    `json:"model_id"`
				Name        string   `json:"name"`
				Images      []string `json:"images"`
				Amount      int      `json:"amount"`
				ItemPrice   float64  `json:"item_price"`
				IsAddOnDeal bool     `json:"is_add_on_deal"`
			} `json:"item"`
			Images []string `json:"images"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get return detail: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	r := resp.Response

	// Map items
	items := make([]providers.ExternalReturnItem, len(r.Item))
	for i, item := range r.Item {
		items[i] = providers.ExternalReturnItem{
			ExternalProductID: fmt.Sprintf("%d", item.ItemID),
			ExternalVariantID: fmt.Sprintf("%d", item.ModelID),
			Name:              item.Name,
			Quantity:          item.Amount,
			Price:             item.ItemPrice,
			Images:            item.Images,
		}
	}

	return &providers.ExternalReturn{
		ExternalReturnID: r.ReturnSN,
		ExternalOrderID:  r.OrderSN,
		Status:           p.mapReturnStatus(r.Status),
		Reason:           r.Reason,
		ReasonText:       r.TextReason,
		RefundAmount:     r.RefundAmount,
		Currency:         r.Currency,
		NeedsLogistics:   r.NeedsLogistics,
		TrackingNumber:   r.TrackingNumber,
		BuyerID:          fmt.Sprintf("%d", r.User.UserID),
		BuyerName:        r.User.Username,
		Items:            items,
		Images:           r.Images,
		CreatedAt:        time.Unix(r.CreateTime, 0),
		UpdatedAt:        time.Unix(r.UpdateTime, 0),
		ShipDueDate:      func() *time.Time { t := time.Unix(r.ReturnShipDue, 0); return &t }(),
	}, nil
}

// ConfirmReturn accepts a return request
func (p *ReturnProvider) ConfirmReturn(ctx context.Context, returnSN string) error {
	req := &Request{
		Method: http.MethodPost,
		Path:   ConfirmReturnPath,
		Body: map[string]interface{}{
			"return_sn": returnSN,
		},
		NeedAuth: true,
	}

	var resp BaseResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to confirm return: %w", err)
	}

	if resp.HasError() {
		return fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return nil
}

// DisputeReturn disputes/rejects a return request
func (p *ReturnProvider) DisputeReturn(ctx context.Context, returnSN string, email string, disputeReason string, images []string) error {
	body := map[string]interface{}{
		"return_sn":      returnSN,
		"email":          email,
		"dispute_reason": disputeReason,
	}

	if len(images) > 0 {
		body["images"] = images
	}

	req := &Request{
		Method:   http.MethodPost,
		Path:     DisputeReturnPath,
		Body:     body,
		NeedAuth: true,
	}

	var resp BaseResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to dispute return: %w", err)
	}

	if resp.HasError() {
		return fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return nil
}

// mapReturnStatus maps Shopee return status to internal status
func (p *ReturnProvider) mapReturnStatus(status string) string {
	switch status {
	case "REQUESTED":
		return "requested"
	case "ACCEPTED":
		return "accepted"
	case "CANCELLED":
		return "cancelled"
	case "JUDGING":
		return "judging"
	case "REFUND_PAID":
		return "refunded"
	case "CLOSED":
		return "closed"
	case "PROCESSING":
		return "processing"
	case "SELLER_DISPUTE":
		return "disputed"
	default:
		return status
	}
}
