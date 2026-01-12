package shopee

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/niaga-platform/service-marketplace/internal/providers"
)

const (
	// Product API paths
	AddItemPath         = "/api/v2/product/add_item"
	UpdateItemPath      = "/api/v2/product/update_item"
	DeleteItemPath      = "/api/v2/product/delete_item"
	GetItemListPath     = "/api/v2/product/get_item_list"
	GetItemInfoPath     = "/api/v2/product/get_item_base_info"
	GetCategoryPath     = "/api/v2/product/get_category"
	UpdateStockPath     = "/api/v2/product/update_stock"
	UploadImagePath     = "/api/v2/media_space/upload_image"
	InitVideoUploadPath = "/api/v2/media_space/init_video_upload"

	// Logistics API paths
	GetLogisticsChannelPath = "/api/v2/logistics/get_channel_list"
)

// ProductProvider implements product operations for Shopee
type ProductProvider struct {
	client *Client
}

// NewProductProvider creates a new Shopee product provider
func NewProductProvider(client *Client) *ProductProvider {
	return &ProductProvider{client: client}
}

// LogisticsChannel represents a Shopee logistics channel
type LogisticsChannel struct {
	LogisticID   int64  `json:"logistics_channel_id"`
	LogisticName string `json:"logistics_channel_name"`
	Enabled      bool   `json:"enabled"`
	CODEnabled   bool   `json:"cod_enabled"`
}

// GetLogisticsChannels fetches available logistics channels for the shop
func (p *ProductProvider) GetLogisticsChannels(ctx context.Context) ([]LogisticsChannel, error) {
	req := &Request{
		Method:   http.MethodGet,
		Path:     GetLogisticsChannelPath,
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			LogisticsChannelList []struct {
				LogisticsChannelID   int64  `json:"logistics_channel_id"`
				LogisticsChannelName string `json:"logistics_channel_name"`
				Enabled              bool   `json:"enabled"`
				CODEnabled           bool   `json:"cod_enabled"`
			} `json:"logistics_channel_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get logistics channels: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	channels := make([]LogisticsChannel, len(resp.Response.LogisticsChannelList))
	for i, ch := range resp.Response.LogisticsChannelList {
		channels[i] = LogisticsChannel{
			LogisticID:   ch.LogisticsChannelID,
			LogisticName: ch.LogisticsChannelName,
			Enabled:      ch.Enabled,
			CODEnabled:   ch.CODEnabled,
		}
	}

	return channels, nil
}

// UploadImageByURL downloads an image from URL and uploads it to Shopee's Media Space
// Returns the image_id that can be used in product creation
func (p *ProductProvider) UploadImageByURL(ctx context.Context, imageURL string) (string, error) {
	// Download the image from the URL
	httpClient := &http.Client{Timeout: 30 * time.Second}
	imgResp, err := httpClient.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download image from %s: %w", imageURL, err)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", imgResp.StatusCode)
	}

	// Read image data
	imageData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image data: %w", err)
	}

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Get filename from URL
	filename := path.Base(imageURL)
	if filename == "" || filename == "." || filename == "/" {
		filename = "image.jpg"
	}

	// Create form file field
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	// Write image data to form
	if _, err := part.Write(imageData); err != nil {
		return "", fmt.Errorf("failed to write image data: %w", err)
	}

	// Close multipart writer
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Use client's multipart upload method
	uploadResp, err := p.client.DoMultipart(ctx, UploadImagePath, writer.FormDataContentType(), &body)
	if err != nil {
		return "", fmt.Errorf("failed to upload image to Shopee: %w", err)
	}

	// Parse response
	var respData struct {
		BaseResponse
		Response struct {
			ImageInfo struct {
				ImageID string `json:"image_id"`
			} `json:"image_info"`
		} `json:"response"`
	}

	if err := json.Unmarshal(uploadResp, &respData); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	if respData.HasError() {
		return "", fmt.Errorf("shopee image upload error: %s", respData.GetError())
	}

	return respData.Response.ImageInfo.ImageID, nil
}

// GetCategories fetches marketplace categories
func (p *ProductProvider) GetCategories(ctx context.Context) ([]providers.ExternalCategory, error) {
	req := &Request{
		Method:   http.MethodGet,
		Path:     GetCategoryPath,
		Query:    map[string]string{"language": "en"},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			CategoryList []struct {
				CategoryID           int64  `json:"category_id"`
				ParentCategoryID     int64  `json:"parent_category_id"`
				OriginalCategoryName string `json:"original_category_name"`
				DisplayCategoryName  string `json:"display_category_name"`
				HasChildren          bool   `json:"has_children"`
			} `json:"category_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get categories: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	categories := make([]providers.ExternalCategory, len(resp.Response.CategoryList))
	for i, cat := range resp.Response.CategoryList {
		parentID := ""
		if cat.ParentCategoryID > 0 {
			parentID = fmt.Sprintf("%d", cat.ParentCategoryID)
		}
		categories[i] = providers.ExternalCategory{
			CategoryID:   fmt.Sprintf("%d", cat.CategoryID),
			CategoryName: cat.DisplayCategoryName,
			ParentID:     parentID,
			IsLeaf:       !cat.HasChildren,
		}
	}

	return categories, nil
}

// isSpamDescription checks if description appears to be placeholder/spam text
func isSpamDescription(desc string) bool {
	if len(desc) < 20 {
		return true
	}
	// Check if description is mostly repeated characters
	charCount := make(map[rune]int)
	for _, r := range desc {
		charCount[r]++
	}
	// If any single character makes up more than 50% of the description, it's spam
	threshold := len(desc) / 2
	for _, count := range charCount {
		if count > threshold {
			return true
		}
	}
	return false
}

// PushProduct creates a new product on Shopee
func (p *ProductProvider) PushProduct(ctx context.Context, product *providers.ProductPushRequest) (*providers.ProductPushResponse, error) {
	// Validate description - Shopee requires meaningful content (min 100 chars, no spam)
	if len(product.Description) < 100 {
		return nil, fmt.Errorf("description too short - Shopee requires at least 100 characters (current: %d)", len(product.Description))
	}

	// Check for spam/placeholder patterns (repeated characters)
	if isSpamDescription(product.Description) {
		return nil, fmt.Errorf("description appears to be placeholder text - Shopee requires meaningful product descriptions")
	}

	// Convert category_id from string to int64 (Shopee requires uint64)
	categoryID, err := strconv.ParseInt(product.CategoryID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid category_id: %w", err)
	}

	// Calculate weight in kg (minimum 0.1kg for Shopee)
	weightKg := product.Weight / 1000
	if weightKg < 0.1 {
		weightKg = 0.1
	}

	// Build request body
	itemBody := map[string]interface{}{
		"original_price":   product.OriginalPrice,
		"description":      product.Description,
		"description_type": "normal", // Required by Shopee API - "normal" for text only
		"item_name":        product.Name,
		"weight":           weightKg,
		"category_id":      categoryID,
		"item_sku":         product.SKU,
		"condition":        "NEW",
		"item_status":      "NORMAL",
	}

	// Add dimension (required by Shopee for shipping)
	// Use actual dimensions if provided, otherwise default to 10x10x5 cm
	length := 10.0
	width := 10.0
	height := 5.0
	if product.Dimensions != nil {
		if product.Dimensions.Length > 0 {
			length = product.Dimensions.Length
		}
		if product.Dimensions.Width > 0 {
			width = product.Dimensions.Width
		}
		if product.Dimensions.Height > 0 {
			height = product.Dimensions.Height
		}
	}
	itemBody["dimension"] = map[string]interface{}{
		"package_length": length,
		"package_width":  width,
		"package_height": height,
	}

	// Add seller_stock - Shopee API v2 requires this format
	itemBody["seller_stock"] = []map[string]interface{}{
		{
			"stock": product.Stock,
		},
	}

	// Add images - must upload to Shopee Media Space first
	// Shopee requires at least 1 image
	if len(product.Images) > 0 {
		imageIDs := make([]string, 0, len(product.Images))
		for _, imageURL := range product.Images {
			// Upload each image to Shopee and get image_id
			imageID, err := p.UploadImageByURL(ctx, imageURL)
			if err != nil {
				// Log error but continue with other images
				fmt.Printf("Warning: failed to upload image %s: %v\n", imageURL, err)
				continue
			}
			if imageID != "" {
				imageIDs = append(imageIDs, imageID)
			}
		}
		if len(imageIDs) > 0 {
			itemBody["image"] = map[string]interface{}{
				"image_id_list": imageIDs,
			}
		} else {
			return nil, fmt.Errorf("failed to upload any images - Shopee requires at least 1 product image")
		}
	} else {
		return nil, fmt.Errorf("no images provided - Shopee requires at least 1 product image")
	}

	// Add brand - Shopee requires brand for most categories
	// Use "No Brand" (brand_id: 0) if no brand specified
	brandName := product.Brand
	if brandName == "" {
		brandName = "No Brand"
	}
	itemBody["brand"] = map[string]interface{}{
		"brand_id":             0,
		"original_brand_name": brandName,
	}

	// Add logistic channels - Required by Shopee
	// Fetch available logistics channels from the shop
	logisticsChannels, err := p.GetLogisticsChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get logistics channels: %w", err)
	}

	// Build logistics info using only enabled channels from the shop
	logisticInfo := make([]map[string]interface{}, 0)
	for _, ch := range logisticsChannels {
		if ch.Enabled {
			logisticInfo = append(logisticInfo, map[string]interface{}{
				"logistic_id": ch.LogisticID,
				"enabled":     true,
			})
		}
	}

	// If no enabled channels found, return error
	if len(logisticInfo) == 0 {
		return nil, fmt.Errorf("no enabled logistics channels found for this shop - please enable shipping channels in Shopee Seller Center")
	}

	itemBody["logistic_info"] = logisticInfo

	// Debug: log the payload being sent
	debugPayload, _ := json.Marshal(itemBody)
	fmt.Printf("DEBUG: Shopee add_item payload: %s\n", string(debugPayload))

	req := &Request{
		Method:   http.MethodPost,
		Path:     AddItemPath,
		Body:     itemBody,
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ItemID int64 `json:"item_id"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to push product: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return &providers.ProductPushResponse{
		ExternalProductID: fmt.Sprintf("%d", resp.Response.ItemID),
		ExternalSKU:       product.SKU,
		Status:            "created",
	}, nil
}

// UpdateProduct updates an existing product on Shopee
func (p *ProductProvider) UpdateProduct(ctx context.Context, externalID string, product *providers.ProductUpdateRequest) error {
	updateBody := map[string]interface{}{
		"item_id": externalID,
	}

	if product.Name != "" {
		updateBody["item_name"] = product.Name
	}
	if product.Description != "" {
		updateBody["description"] = product.Description
	}
	if product.Price != nil {
		updateBody["price_info"] = []map[string]interface{}{
			{"current_price": *product.Price},
		}
	}

	req := &Request{
		Method:   http.MethodPost,
		Path:     UpdateItemPath,
		Body:     updateBody,
		NeedAuth: true,
	}

	var resp BaseResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update product: %w", err)
	}

	if resp.HasError() {
		return fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return nil
}

// DeleteProduct deletes a product from Shopee
func (p *ProductProvider) DeleteProduct(ctx context.Context, externalID string) error {
	req := &Request{
		Method: http.MethodPost,
		Path:   DeleteItemPath,
		Body: map[string]interface{}{
			"item_id": externalID,
		},
		NeedAuth: true,
	}

	var resp BaseResponse
	if err := p.client.Do(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete product: %w", err)
	}

	if resp.HasError() {
		return fmt.Errorf("shopee error: %s", resp.GetError())
	}

	return nil
}

// UpdateInventory updates stock for products
func (p *ProductProvider) UpdateInventory(ctx context.Context, updates []providers.InventoryUpdate) error {
	for _, update := range updates {
		req := &Request{
			Method: http.MethodPost,
			Path:   UpdateStockPath,
			Body: map[string]interface{}{
				"item_id": update.ExternalProductID,
				"stock_list": []map[string]interface{}{
					{
						"model_id":     0, // Main product, not variation
						"normal_stock": update.Quantity,
					},
				},
			},
			NeedAuth: true,
		}

		var resp BaseResponse
		if err := p.client.Do(ctx, req, &resp); err != nil {
			return fmt.Errorf("failed to update inventory for %s: %w", update.ExternalProductID, err)
		}

		if resp.HasError() {
			return fmt.Errorf("shopee error for %s: %s", update.ExternalProductID, resp.GetError())
		}
	}

	return nil
}

// GetInventory fetches inventory levels for products
func (p *ProductProvider) GetInventory(ctx context.Context, externalProductIDs []string) ([]providers.InventoryItem, error) {
	// Build comma-separated item IDs
	itemIDList := ""
	for i, id := range externalProductIDs {
		if i > 0 {
			itemIDList += ","
		}
		itemIDList += id
	}

	req := &Request{
		Method: http.MethodGet,
		Path:   GetItemInfoPath,
		Query: map[string]string{
			"item_id_list": itemIDList,
		},
		NeedAuth: true,
	}

	var resp struct {
		BaseResponse
		Response struct {
			ItemList []struct {
				ItemID      int64 `json:"item_id"`
				StockInfoV2 struct {
					SummaryInfo struct {
						TotalAvailableStock int `json:"total_available_stock"`
						TotalReservedStock  int `json:"total_reserved_stock"`
					} `json:"summary_info"`
				} `json:"stock_info_v2"`
			} `json:"item_list"`
		} `json:"response"`
	}

	if err := p.client.Do(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get inventory: %w", err)
	}

	if resp.HasError() {
		return nil, fmt.Errorf("shopee error: %s", resp.GetError())
	}

	items := make([]providers.InventoryItem, len(resp.Response.ItemList))
	for i, item := range resp.Response.ItemList {
		items[i] = providers.InventoryItem{
			ExternalProductID: fmt.Sprintf("%d", item.ItemID),
			Quantity:          item.StockInfoV2.SummaryInfo.TotalAvailableStock,
			Reserved:          item.StockInfoV2.SummaryInfo.TotalReservedStock,
		}
	}

	return items, nil
}
