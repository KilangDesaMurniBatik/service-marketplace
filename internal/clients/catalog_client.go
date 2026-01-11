package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// CatalogClient handles communication with service-catalog
type CatalogClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewCatalogClient creates a new CatalogClient
func NewCatalogClient(baseURL string, logger *zap.Logger) *CatalogClient {
	return &CatalogClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// Product represents a product from service-catalog
type Product struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Description   string            `json:"description"`
	BasePrice     float64           `json:"base_price"`
	SalePrice     *float64          `json:"sale_price"`
	SKU           string            `json:"sku"`
	Weight        float64           `json:"weight"`
	Dimensions    *ProductDimension `json:"dimensions"`
	CategoryID    string            `json:"category_id"`
	CategoryName  string            `json:"category_name"`
	Brand         string            `json:"brand"`
	Status        string            `json:"status"`
	Images        []ProductImage    `json:"images"`
	Variants      []ProductVariant  `json:"variants"`
	StockQuantity int               `json:"stock_quantity"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// ProductDimension represents product dimensions for shipping
type ProductDimension struct {
	Length float64 `json:"length"` // cm
	Width  float64 `json:"width"`  // cm
	Height float64 `json:"height"` // cm
}

// ProductImage represents a product image
type ProductImage struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	AltText   string `json:"alt_text"`
	IsPrimary bool   `json:"is_primary"`
	SortOrder int    `json:"sort_order"`
}

// ProductVariant represents a product variant
type ProductVariant struct {
	ID            string   `json:"id"`
	SKU           string   `json:"sku"`
	Name          string   `json:"name"`
	Price         float64  `json:"price"`
	StockQuantity int      `json:"stock_quantity"`
	Options       []Option `json:"options"`
}

// Option represents a variant option
type Option struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Category represents a category from service-catalog
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	ParentID string `json:"parent_id"`
}

// GetProduct fetches a product by ID
// Uses public catalog endpoint for service-to-service communication
func (c *CatalogClient) GetProduct(ctx context.Context, productID string) (*Product, error) {
	url := fmt.Sprintf("%s/api/v1/catalog/products/%s", c.baseURL, productID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Public catalog endpoint returns {success, message, data} format
	var result struct {
		Success bool    `json:"success"`
		Message string  `json:"message"`
		Data    Product `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result.Data, nil
}

// GetProducts fetches multiple products by IDs
func (c *CatalogClient) GetProducts(ctx context.Context, productIDs []string) ([]Product, error) {
	products := make([]Product, 0, len(productIDs))

	for _, id := range productIDs {
		product, err := c.GetProduct(ctx, id)
		if err != nil {
			c.logger.Warn("Failed to fetch product", zap.String("id", id), zap.Error(err))
			continue
		}
		products = append(products, *product)
	}

	return products, nil
}

// GetAllProducts fetches all active products from catalog (for Push All feature)
// Uses public catalog endpoint for service-to-service communication
func (c *CatalogClient) GetAllProducts(ctx context.Context) ([]Product, error) {
	url := fmt.Sprintf("%s/api/v1/catalog/products?status=active&page_size=1000", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Public catalog endpoint returns {success, message, data} format
	var result struct {
		Success bool      `json:"success"`
		Message string    `json:"message"`
		Data    []Product `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("Fetched products from catalog", zap.Int("count", len(result.Data)))
	return result.Data, nil
}

// GetCategories fetches all categories
func (c *CatalogClient) GetCategories(ctx context.Context) ([]Category, error) {
	url := fmt.Sprintf("%s/api/v1/catalog/categories", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Categories []Category `json:"categories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Categories, nil
}

// GetCategory fetches a category by ID
func (c *CatalogClient) GetCategory(ctx context.Context, categoryID string) (*Category, error) {
	url := fmt.Sprintf("%s/api/v1/catalog/categories/%s", c.baseURL, categoryID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Category Category `json:"category"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result.Category, nil
}
