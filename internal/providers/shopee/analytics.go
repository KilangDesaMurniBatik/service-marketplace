package shopee

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

// ShopPerformanceResponse represents the Shopee shop performance API response
type ShopPerformanceResponse struct {
	BaseResponse
	Response struct {
		TotalViews           int64   `json:"total_views"`
		TotalVisitors        int64   `json:"total_visitors"`
		TotalOrders          int64   `json:"total_orders"`
		TotalSales           float64 `json:"total_sales"`
		TotalProducts        int     `json:"total_products"`
		TotalLiveProducts    int     `json:"total_live_products"`
		ConversionRate       float64 `json:"conversion_rate"`
		ResponseRate         float64 `json:"response_rate"`
		ChatResponseTime     int     `json:"chat_response_time"`
		LateShipmentRate     float64 `json:"late_shipment_rate"`
		CancellationRate     float64 `json:"cancellation_rate"`
		ReturnRefundRate     float64 `json:"return_refund_rate"`
		PreparationTime      int     `json:"preparation_time"`
		OverallShopRating    float64 `json:"overall_shop_rating"`
		PenaltyPoints        int     `json:"penalty_points"`
		NonFulfillmentRate   float64 `json:"non_fulfillment_rate"`
	} `json:"response"`
}

// ShopDailyPerformanceResponse represents daily performance data
type ShopDailyPerformanceResponse struct {
	BaseResponse
	Response struct {
		Data []struct {
			Date       int64   `json:"date"`
			Views      int64   `json:"views"`
			Visitors   int64   `json:"visitors"`
			Orders     int64   `json:"orders"`
			Sales      float64 `json:"sales"`
			Conversion float64 `json:"conversion_rate"`
		} `json:"data"`
	} `json:"response"`
}

// TopSellingProductsResponse represents top selling products
type TopSellingProductsResponse struct {
	BaseResponse
	Response struct {
		ItemList []struct {
			ItemID     int64   `json:"item_id"`
			ItemName   string  `json:"item_name"`
			ImageURL   string  `json:"image_url"`
			SoldCount  int64   `json:"sold_count"`
			Revenue    float64 `json:"revenue"`
			Views      int64   `json:"views"`
			Conversion float64 `json:"conversion_rate"`
		} `json:"item_list"`
	} `json:"response"`
}

// GetShopPerformance gets overall shop performance metrics
func (p *Provider) GetShopPerformance(ctx context.Context, params providers.AnalyticsQueryParams) (*providers.ShopPerformance, error) {
	startTs := params.StartDate.Unix()
	endTs := params.EndDate.Unix()

	req := &Request{
		Method:   http.MethodGet,
		Path:     "/api/v2/shop/performance",
		NeedAuth: true,
		Query: map[string]string{
			"start_time": strconv.FormatInt(startTs, 10),
			"end_time":   strconv.FormatInt(endTs, 10),
		},
	}

	var resp ShopPerformanceResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		// If performance API fails, return calculated metrics from orders
		return p.calculatePerformanceFromOrders(ctx, params)
	}

	if resp.HasError() {
		// Fallback to order-based calculation
		return p.calculatePerformanceFromOrders(ctx, params)
	}

	avgOrderValue := float64(0)
	if resp.Response.TotalOrders > 0 {
		avgOrderValue = resp.Response.TotalSales / float64(resp.Response.TotalOrders)
	}

	return &providers.ShopPerformance{
		TotalOrders:       resp.Response.TotalOrders,
		TotalSales:        resp.Response.TotalSales,
		TotalViews:        resp.Response.TotalViews,
		TotalVisitors:     resp.Response.TotalVisitors,
		ConversionRate:    resp.Response.ConversionRate,
		AverageOrderValue: avgOrderValue,
		TotalProducts:     resp.Response.TotalProducts,
		ActiveProducts:    resp.Response.TotalLiveProducts,
		Currency:          "MYR",
	}, nil
}

// GetDailySales gets daily sales data
func (p *Provider) GetDailySales(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.DailySales, error) {
	// Try to get daily performance from Shopee API
	startTs := params.StartDate.Unix()
	endTs := params.EndDate.Unix()

	req := &Request{
		Method:   http.MethodGet,
		Path:     "/api/v2/shop/get_shop_performance",
		NeedAuth: true,
		Query: map[string]string{
			"start_time": strconv.FormatInt(startTs, 10),
			"end_time":   strconv.FormatInt(endTs, 10),
			"page_size":  "30",
		},
	}

	var resp ShopDailyPerformanceResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		// Calculate from orders
		return p.calculateDailySalesFromOrders(ctx, params)
	}

	if resp.HasError() {
		return p.calculateDailySalesFromOrders(ctx, params)
	}

	result := make([]providers.DailySales, 0, len(resp.Response.Data))
	for _, d := range resp.Response.Data {
		date := time.Unix(d.Date, 0).Format("2006-01-02")
		result = append(result, providers.DailySales{
			Date:       date,
			Orders:     d.Orders,
			Sales:      d.Sales,
			Views:      d.Views,
			Visitors:   d.Visitors,
			Conversion: d.Conversion,
		})
	}

	return result, nil
}

// GetTopProducts gets top-selling products
func (p *Provider) GetTopProducts(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.TopProduct, error) {
	startTs := params.StartDate.Unix()
	endTs := params.EndDate.Unix()

	limit := params.Limit
	if limit == 0 {
		limit = 10
	}

	req := &Request{
		Method:   http.MethodGet,
		Path:     "/api/v2/product/get_item_list",
		NeedAuth: true,
		Query: map[string]string{
			"offset":     "0",
			"page_size":  strconv.Itoa(limit),
			"item_status": "NORMAL",
			"sort_by":    "sales",
		},
	}

	// Get product list sorted by sales
	var productResp struct {
		BaseResponse
		Response struct {
			ItemList []struct {
				ItemID int64 `json:"item_id"`
			} `json:"item"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &productResp); err != nil {
		return nil, err
	}

	if len(productResp.Response.ItemList) == 0 {
		return []providers.TopProduct{}, nil
	}

	// Get detailed info for top products
	itemIDs := make([]int64, 0, len(productResp.Response.ItemList))
	for _, item := range productResp.Response.ItemList {
		itemIDs = append(itemIDs, item.ItemID)
	}

	// Get item base info
	baseInfoReq := &Request{
		Method:   http.MethodGet,
		Path:     "/api/v2/product/get_item_base_info",
		NeedAuth: true,
		Query: map[string]string{
			"item_id_list": joinInt64s(itemIDs),
		},
	}

	var baseInfoResp struct {
		BaseResponse
		Response struct {
			ItemList []struct {
				ItemID    int64  `json:"item_id"`
				ItemName  string `json:"item_name"`
				Image     struct {
					ImageURLList []string `json:"image_url_list"`
				} `json:"image"`
				SaleInfo struct {
					TotalSales int64   `json:"total_sales"`
				} `json:"sales_info,omitempty"`
			} `json:"item_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, baseInfoReq, &baseInfoResp); err != nil {
		return nil, err
	}

	// Get extra info (views, sold count)
	extraReq := &Request{
		Method:   http.MethodGet,
		Path:     "/api/v2/product/get_item_extra_info",
		NeedAuth: true,
		Query: map[string]string{
			"item_id_list": joinInt64s(itemIDs),
			"start_time":   strconv.FormatInt(startTs, 10),
			"end_time":     strconv.FormatInt(endTs, 10),
		},
	}

	var extraResp struct {
		BaseResponse
		Response struct {
			ItemList []struct {
				ItemID   int64   `json:"item_id"`
				Views    int64   `json:"views"`
				Sold     int64   `json:"sold"`
				Revenue  float64 `json:"revenue"`
			} `json:"item_list"`
		} `json:"response"`
	}

	extraMap := make(map[int64]struct {
		Views   int64
		Sold    int64
		Revenue float64
	})

	if err := p.client.Do(ctx, extraReq, &extraResp); err == nil {
		for _, item := range extraResp.Response.ItemList {
			extraMap[item.ItemID] = struct {
				Views   int64
				Sold    int64
				Revenue float64
			}{
				Views:   item.Views,
				Sold:    item.Sold,
				Revenue: item.Revenue,
			}
		}
	}

	result := make([]providers.TopProduct, 0, len(baseInfoResp.Response.ItemList))
	for _, item := range baseInfoResp.Response.ItemList {
		imageURL := ""
		if len(item.Image.ImageURLList) > 0 {
			imageURL = item.Image.ImageURLList[0]
		}

		extra := extraMap[item.ItemID]
		conversionRate := float64(0)
		if extra.Views > 0 {
			conversionRate = float64(extra.Sold) / float64(extra.Views) * 100
		}

		result = append(result, providers.TopProduct{
			ExternalProductID: strconv.FormatInt(item.ItemID, 10),
			Name:              item.ItemName,
			ImageURL:          imageURL,
			TotalSold:         extra.Sold,
			TotalRevenue:      extra.Revenue,
			Views:             extra.Views,
			ConversionRate:    conversionRate,
		})
	}

	return result, nil
}

// GetTrafficSources gets traffic source analytics
func (p *Provider) GetTrafficSources(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.TrafficSource, error) {
	// Shopee doesn't provide detailed traffic source API publicly
	// Return a placeholder with Shopee as the source
	return []providers.TrafficSource{
		{
			Source:  "shopee",
			Views:   0,
			Orders:  0,
			Sales:   0,
			Percent: 100,
		},
	}, nil
}

// calculatePerformanceFromOrders calculates performance metrics from order data
func (p *Provider) calculatePerformanceFromOrders(ctx context.Context, params providers.AnalyticsQueryParams) (*providers.ShopPerformance, error) {
	// Get orders within the date range
	orders, err := p.GetOrders(ctx, providers.OrderQueryParams{
		StartTime: &params.StartDate,
		EndTime:   &params.EndDate,
		PageSize:  100,
	})
	if err != nil {
		return &providers.ShopPerformance{Currency: "MYR"}, nil
	}

	totalOrders := int64(len(orders))
	totalSales := float64(0)
	for _, order := range orders {
		totalSales += order.TotalAmount
	}

	avgOrderValue := float64(0)
	if totalOrders > 0 {
		avgOrderValue = totalSales / float64(totalOrders)
	}

	return &providers.ShopPerformance{
		TotalOrders:       totalOrders,
		TotalSales:        totalSales,
		AverageOrderValue: avgOrderValue,
		Currency:          "MYR",
	}, nil
}

// calculateDailySalesFromOrders calculates daily sales from order data
func (p *Provider) calculateDailySalesFromOrders(ctx context.Context, params providers.AnalyticsQueryParams) ([]providers.DailySales, error) {
	orders, err := p.GetOrders(ctx, providers.OrderQueryParams{
		StartTime: &params.StartDate,
		EndTime:   &params.EndDate,
		PageSize:  200,
	})
	if err != nil {
		return []providers.DailySales{}, nil
	}

	// Group orders by date
	dailyMap := make(map[string]*providers.DailySales)
	for _, order := range orders {
		date := order.CreatedAt.Format("2006-01-02")
		if daily, ok := dailyMap[date]; ok {
			daily.Orders++
			daily.Sales += order.TotalAmount
		} else {
			dailyMap[date] = &providers.DailySales{
				Date:   date,
				Orders: 1,
				Sales:  order.TotalAmount,
			}
		}
	}

	result := make([]providers.DailySales, 0, len(dailyMap))
	for _, daily := range dailyMap {
		result = append(result, *daily)
	}

	return result, nil
}

// joinInt64s joins int64 slice into comma-separated string
func joinInt64s(nums []int64) string {
	if len(nums) == 0 {
		return ""
	}
	result := strconv.FormatInt(nums[0], 10)
	for i := 1; i < len(nums); i++ {
		result += "," + strconv.FormatInt(nums[i], 10)
	}
	return result
}
