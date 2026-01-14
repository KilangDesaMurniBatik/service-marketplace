package providers

import "time"

// ShopPerformance represents overall shop performance metrics
type ShopPerformance struct {
	TotalOrders       int64   `json:"total_orders"`
	TotalSales        float64 `json:"total_sales"`
	TotalViews        int64   `json:"total_views"`
	TotalVisitors     int64   `json:"total_visitors"`
	ConversionRate    float64 `json:"conversion_rate"`
	AverageOrderValue float64 `json:"average_order_value"`
	TotalProducts     int     `json:"total_products"`
	ActiveProducts    int     `json:"active_products"`
	Currency          string  `json:"currency"`
}

// DailySales represents daily sales data
type DailySales struct {
	Date       string  `json:"date"`
	Orders     int64   `json:"orders"`
	Sales      float64 `json:"sales"`
	Views      int64   `json:"views"`
	Visitors   int64   `json:"visitors"`
	Conversion float64 `json:"conversion"`
}

// TopProduct represents a top-selling product
type TopProduct struct {
	ExternalProductID string  `json:"external_product_id"`
	Name              string  `json:"name"`
	ImageURL          string  `json:"image_url"`
	TotalSold         int64   `json:"total_sold"`
	TotalRevenue      float64 `json:"total_revenue"`
	Views             int64   `json:"views"`
	ConversionRate    float64 `json:"conversion_rate"`
}

// TrafficSource represents traffic source analytics
type TrafficSource struct {
	Source   string  `json:"source"`
	Views    int64   `json:"views"`
	Orders   int64   `json:"orders"`
	Sales    float64 `json:"sales"`
	Percent  float64 `json:"percent"`
}

// AnalyticsQueryParams represents parameters for querying analytics
type AnalyticsQueryParams struct {
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	Limit     int       `json:"limit,omitempty"`
}

// AnalyticsResponse represents the combined analytics response
type AnalyticsResponse struct {
	Performance   *ShopPerformance `json:"performance"`
	DailySales    []DailySales     `json:"daily_sales"`
	TopProducts   []TopProduct     `json:"top_products"`
	TrafficSource []TrafficSource  `json:"traffic_sources"`
}
