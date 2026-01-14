package providers

import (
	"time"
)

// ReturnListParams represents parameters for listing returns
type ReturnListParams struct {
	CreateTimeFrom time.Time `json:"create_time_from,omitempty"`
	CreateTimeTo   time.Time `json:"create_time_to,omitempty"`
	Status         string    `json:"status,omitempty"`
	PageSize       int       `json:"page_size"`
	PageNo         int       `json:"page_no,omitempty"`
	Cursor         string    `json:"cursor,omitempty"`
}

// ExternalReturn represents a return/refund request from marketplace
type ExternalReturn struct {
	ExternalReturnID string              `json:"external_return_id"`
	ExternalOrderID  string              `json:"external_order_id"`
	Status           string              `json:"status"`
	Reason           string              `json:"reason"`
	ReasonText       string              `json:"reason_text,omitempty"`
	RefundAmount     float64             `json:"refund_amount"`
	Currency         string              `json:"currency"`
	NeedsLogistics   bool                `json:"needs_logistics"`
	TrackingNumber   string              `json:"tracking_number,omitempty"`
	BuyerID          string              `json:"buyer_id"`
	BuyerName        string              `json:"buyer_name"`
	Items            []ExternalReturnItem `json:"items"`
	Images           []string            `json:"images,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	ShipDueDate      *time.Time          `json:"ship_due_date,omitempty"`
}

// ExternalReturnItem represents an item in a return request
type ExternalReturnItem struct {
	ExternalProductID string   `json:"external_product_id"`
	ExternalVariantID string   `json:"external_variant_id,omitempty"`
	Name              string   `json:"name"`
	Quantity          int      `json:"quantity"`
	Price             float64  `json:"price"`
	Images            []string `json:"images,omitempty"`
}

// Return status constants
const (
	ReturnStatusRequested  = "requested"
	ReturnStatusAccepted   = "accepted"
	ReturnStatusCancelled  = "cancelled"
	ReturnStatusJudging    = "judging"
	ReturnStatusRefunded   = "refunded"
	ReturnStatusClosed     = "closed"
	ReturnStatusProcessing = "processing"
	ReturnStatusDisputed   = "disputed"
)

// Return reason constants (Shopee standard reasons)
const (
	ReturnReasonDidNotReceive       = "DID_NOT_RECEIVE_GOODS"
	ReturnReasonIncomplete          = "INCOMPLETE_PRODUCT"
	ReturnReasonWrongItem           = "WRONG_ITEM"
	ReturnReasonDamaged             = "PHYSICAL_DAMAGE"
	ReturnReasonFaulty              = "FAULTY_PRODUCT"
	ReturnReasonDifferentFromDesc   = "DIFFERENT_FROM_DESCRIPTION"
	ReturnReasonCounterfeit         = "COUNTERFEIT_PRODUCT"
	ReturnReasonChangeOfMind        = "CHANGE_OF_MIND"
)

// DisputeReturnRequest represents a request to dispute a return
type DisputeReturnRequest struct {
	ReturnID      string   `json:"return_id"`
	Email         string   `json:"email"`
	DisputeReason string   `json:"dispute_reason"`
	Images        []string `json:"images,omitempty"`
}
