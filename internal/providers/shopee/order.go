package shopee

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

const (
	GetOrderListPath              = "/api/v2/order/get_order_list"
	GetOrderDetailPath            = "/api/v2/order/get_order_detail"
	ShipOrderPath                 = "/api/v2/logistics/ship_order"
	GetShippingParameterPath      = "/api/v2/logistics/get_shipping_parameter"
	GetShippingDocumentParamPath  = "/api/v2/logistics/get_shipping_document_parameter"
	CreateShippingDocumentPath    = "/api/v2/logistics/create_shipping_document"
	GetShippingDocumentResultPath = "/api/v2/logistics/get_shipping_document_result"
	DownloadShippingDocumentPath  = "/api/v2/logistics/download_shipping_document"
)

// OrderProvider implements order operations for Shopee
type OrderProvider struct {
	client *Client
}

// NewOrderProvider creates a new Shopee order provider
func NewOrderProvider(client *Client) *OrderProvider {
	return &OrderProvider{client: client}
}

// GetOrders fetches orders from Shopee
func (p *OrderProvider) GetOrders(ctx context.Context, params *providers.OrderListParams) ([]providers.ExternalOrder, string, error) {
	query := map[string]string{
		"time_range_field": "create_time",
		"time_from":        fmt.Sprintf("%d", params.TimeFrom.Unix()),
		"time_to":          fmt.Sprintf("%d", params.TimeTo.Unix()),
		"page_size":        fmt.Sprintf("%d", params.PageSize),
	}

	if params.Cursor != "" {
		query["cursor"] = params.Cursor
	}
	if params.Status != "" {
		query["order_status"] = params.Status
	}

	req := &Request{
		Method:   http.MethodGet,
		Path:     GetOrderListPath,
		Query:    query,
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			More       bool   `json:"more"`
			NextCursor string `json:"next_cursor"`
			OrderList  []struct {
				OrderSN string `json:"order_sn"`
			} `json:"order_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, "", fmt.Errorf("failed to get orders: %w", err)
	}

	if resp.HasError() {
		return nil, "", fmt.Errorf("shopee error: %s", resp.GetError())
	}

	if len(resp.Response.OrderList) == 0 {
		return []providers.ExternalOrder{}, "", nil
	}

	// Fetch order details
	orderSNs := make([]string, len(resp.Response.OrderList))
	for i, o := range resp.Response.OrderList {
		orderSNs[i] = o.OrderSN
	}

	orders, err := p.getOrderDetails(ctx, orderSNs)
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if resp.Response.More {
		nextCursor = resp.Response.NextCursor
	}

	return orders, nextCursor, nil
}

// getOrderDetails fetches detailed order information
func (p *OrderProvider) getOrderDetails(ctx context.Context, orderSNs []string) ([]providers.ExternalOrder, error) {
	orderSNList := ""
	for i, sn := range orderSNs {
		if i > 0 {
			orderSNList += ","
		}
		orderSNList += sn
	}

	req := &Request{
		Method: http.MethodGet,
		Path:   GetOrderDetailPath,
		Query: map[string]string{
			"order_sn_list":            orderSNList,
			"response_optional_fields": "buyer_user_id,buyer_username,item_list,recipient_address",
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			OrderList []struct {
				OrderSN          string  `json:"order_sn"`
				OrderStatus      string  `json:"order_status"`
				CreateTime       int64   `json:"create_time"`
				UpdateTime       int64   `json:"update_time"`
				PayTime          int64   `json:"pay_time"`
				TotalAmount      float64 `json:"total_amount"`
				Currency         string  `json:"currency"`
				BuyerUserID      int64   `json:"buyer_user_id"`
				BuyerUsername    string  `json:"buyer_username"`
				ShippingCarrier  string  `json:"shipping_carrier"`
				TrackingNumber   string  `json:"tracking_number"`
				RecipientAddress struct {
					Name        string `json:"name"`
					Phone       string `json:"phone"`
					Town        string `json:"town"`
					District    string `json:"district"`
					City        string `json:"city"`
					State       string `json:"state"`
					Region      string `json:"region"`
					Zipcode     string `json:"zipcode"`
					FullAddress string `json:"full_address"`
				} `json:"recipient_address"`
				ItemList []struct {
					ItemID               int64   `json:"item_id"`
					ItemName             string  `json:"item_name"`
					ItemSKU              string  `json:"item_sku"`
					ModelID              int64   `json:"model_id"`
					ModelName            string  `json:"model_name"`
					ModelSKU             string  `json:"model_sku"`
					ModelQuantity        int     `json:"model_quantity_purchased"`
					ModelOriginalPrice   float64 `json:"model_original_price"`
					ModelDiscountedPrice float64 `json:"model_discounted_price"`
				} `json:"item_list"`
			} `json:"order_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get order details: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	orders := make([]providers.ExternalOrder, len(resp.Response.OrderList))
	for i, o := range resp.Response.OrderList {
		items := make([]providers.ExternalOrderItem, len(o.ItemList))
		for j, item := range o.ItemList {
			items[j] = providers.ExternalOrderItem{
				ExternalProductID: fmt.Sprintf("%d", item.ItemID),
				ExternalSKU:       item.ModelSKU,
				Name:              item.ItemName,
				Quantity:          item.ModelQuantity,
				UnitPrice:         item.ModelDiscountedPrice,
				TotalPrice:        item.ModelDiscountedPrice * float64(item.ModelQuantity),
			}
		}

		orders[i] = providers.ExternalOrder{
			ExternalOrderID: o.OrderSN,
			Status:          p.mapOrderStatus(o.OrderStatus),
			TotalAmount:     o.TotalAmount,
			Currency:        o.Currency,
			CreatedAt:       time.Unix(o.CreateTime, 0),
			UpdatedAt:       time.Unix(o.UpdateTime, 0),
			PaidAt:          func() *time.Time { t := time.Unix(o.PayTime, 0); return &t }(),
			BuyerName:       o.BuyerUsername,
			BuyerID:         fmt.Sprintf("%d", o.BuyerUserID),
			ShippingAddress: providers.ShippingAddress{
				Name:    o.RecipientAddress.Name,
				Phone:   o.RecipientAddress.Phone,
				City:    o.RecipientAddress.City,
				State:   o.RecipientAddress.State,
				Country: o.RecipientAddress.Region,
				ZipCode: o.RecipientAddress.Zipcode,
				Address: o.RecipientAddress.FullAddress,
			},
			Items:          items,
			TrackingNumber: o.TrackingNumber,
			Carrier:        o.ShippingCarrier,
		}
	}

	return orders, nil
}

func (p *OrderProvider) mapOrderStatus(status string) string {
	switch status {
	case "UNPAID":
		return "pending_payment"
	case "READY_TO_SHIP":
		return "pending_shipment"
	case "PROCESSED":
		return "processing"
	case "SHIPPED":
		return "shipped"
	case "COMPLETED":
		return "completed"
	case "CANCELLED":
		return "cancelled"
	case "IN_CANCEL":
		return "cancellation_requested"
	default:
		return status
	}
}

// GetOrder fetches a single order
func (p *OrderProvider) GetOrder(ctx context.Context, orderID string) (*providers.ExternalOrder, error) {
	orders, err := p.getOrderDetails(ctx, []string{orderID})
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	return &orders[0], nil
}

// UpdateOrderStatus updates order status (e.g., ship order)
func (p *OrderProvider) UpdateOrderStatus(ctx context.Context, orderID, status string) error {
	if status == "shipped" {
		req := &Request{
			Method: http.MethodPost,
			Path:   ShipOrderPath,
			Body: map[string]interface{}{
				"order_sn": orderID,
			},
			NeedAuth: true,
		}

		var resp BaseResponse
		if err := p.client.Do(ctx, req, &resp); err != nil {
			return fmt.Errorf("failed to ship order: %w", err)
		}

		if resp.HasError() {
			return fmt.Errorf("shopee error: %s", resp.GetError())
		}
	}

	return nil
}

// ShippingDocumentInfo contains AWB download information
type ShippingDocumentInfo struct {
	Status      string `json:"status"`
	DownloadURL string `json:"download_url"`
	ErrorMsg    string `json:"error_msg,omitempty"`
}

// GetShippingParameter gets shipping parameters for an order
func (p *OrderProvider) GetShippingParameter(ctx context.Context, orderSN string) (map[string]interface{}, error) {
	req := &Request{
		Method: http.MethodGet,
		Path:   GetShippingParameterPath,
		Query: map[string]string{
			"order_sn": orderSN,
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			InfoNeeded struct {
				Dropoff   []string `json:"dropoff"`
				Pickup    []string `json:"pickup"`
				NonIntegrated []string `json:"non_integrated"`
			} `json:"info_needed"`
			Dropoff   []map[string]interface{} `json:"dropoff"`
			Pickup    []map[string]interface{} `json:"pickup"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get shipping parameter: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return map[string]interface{}{
		"info_needed": resp.Response.InfoNeeded,
		"dropoff":     resp.Response.Dropoff,
		"pickup":      resp.Response.Pickup,
	}, nil
}

// CreateShippingDocument creates AWB document for an order
func (p *OrderProvider) CreateShippingDocument(ctx context.Context, orderSN string, documentType string) error {
	// Document types: NORMAL_AIR_WAYBILL, THERMAL_AIR_WAYBILL, NORMAL_JOB_AIR_WAYBILL, THERMAL_JOB_AIR_WAYBILL
	if documentType == "" {
		documentType = "NORMAL_AIR_WAYBILL"
	}

	req := &Request{
		Method: http.MethodPost,
		Path:   CreateShippingDocumentPath,
		Body: map[string]interface{}{
			"order_list": []map[string]interface{}{
				{
					"order_sn":               orderSN,
					"shipping_document_type": documentType,
				},
			},
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ResultList []struct {
				OrderSN   string `json:"order_sn"`
				FailError string `json:"fail_error"`
			} `json:"result_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to create shipping document: %w", err)
	}

	if resp.HasError() {
		return fmt.Errorf("shopee error: %s", resp.GetError())
	}

	// Check for per-order errors
	for _, result := range resp.Response.ResultList {
		if result.FailError != "" {
			return fmt.Errorf("failed to create document for %s: %s", result.OrderSN, result.FailError)
		}
	}

	return nil
}

// GetShippingDocumentResult checks if AWB is ready to download
func (p *OrderProvider) GetShippingDocumentResult(ctx context.Context, orderSN string, documentType string) (*ShippingDocumentInfo, error) {
	if documentType == "" {
		documentType = "NORMAL_AIR_WAYBILL"
	}

	req := &Request{
		Method: http.MethodPost,
		Path:   GetShippingDocumentResultPath,
		Body: map[string]interface{}{
			"order_list": []map[string]interface{}{
				{
					"order_sn":               orderSN,
					"shipping_document_type": documentType,
				},
			},
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ResultList []struct {
				OrderSN   string `json:"order_sn"`
				Status    string `json:"status"`
				FailError string `json:"fail_error"`
			} `json:"result_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get shipping document result: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	for _, result := range resp.Response.ResultList {
		if result.OrderSN == orderSN {
			return &ShippingDocumentInfo{
				Status:   result.Status, // READY, FAILED, PROCESSING
				ErrorMsg: result.FailError,
			}, nil
		}
	}

	return nil, fmt.Errorf("order not found in result")
}

// DownloadShippingDocument downloads AWB PDF for an order
func (p *OrderProvider) DownloadShippingDocument(ctx context.Context, orderSN string, documentType string) (string, error) {
	if documentType == "" {
		documentType = "NORMAL_AIR_WAYBILL"
	}

	req := &Request{
		Method: http.MethodPost,
		Path:   DownloadShippingDocumentPath,
		Body: map[string]interface{}{
			"order_list": []map[string]interface{}{
				{
					"order_sn":               orderSN,
					"shipping_document_type": documentType,
				},
			},
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ResultList []struct {
				OrderSN            string `json:"order_sn"`
				ShippingDocumentInfo struct {
					ShippingDocumentURL string `json:"shipping_document_url"`
				} `json:"shipping_document_info"`
				FailError string `json:"fail_error"`
			} `json:"result_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return "", fmt.Errorf("failed to download shipping document: %w", err)
	}

	if resp.HasError() {
		return "", fmt.Errorf("shopee error: %s", resp.GetError())
	}

	for _, result := range resp.Response.ResultList {
		if result.OrderSN == orderSN {
			if result.FailError != "" {
				return "", fmt.Errorf("download failed: %s", result.FailError)
			}
			return result.ShippingDocumentInfo.ShippingDocumentURL, nil
		}
	}

	return "", fmt.Errorf("order not found in result")
}
