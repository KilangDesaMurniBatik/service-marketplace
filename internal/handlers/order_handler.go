package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/niaga-platform/service-marketplace/internal/models"
	"github.com/niaga-platform/service-marketplace/internal/services"
)

// OrderHandler handles order sync API requests
type OrderHandler struct {
	service *services.OrderSyncService
	logger  *zap.Logger
}

// NewOrderHandler creates a new OrderHandler
func NewOrderHandler(service *services.OrderSyncService, logger *zap.Logger) *OrderHandler {
	return &OrderHandler{
		service: service,
		logger:  logger,
	}
}

// GetOrders lists marketplace orders for a connection
// GET /api/v1/admin/marketplace/connections/:id/orders
func (h *OrderHandler) GetOrders(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	filter := &models.MarketplaceOrderFilter{
		Page:     1,
		PageSize: 20,
	}

	if status := c.Query("status"); status != "" {
		filter.Status = status
	}
	if pageStr := c.Query("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			filter.Page = page
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if pageSize, err := strconv.Atoi(pageSizeStr); err == nil && pageSize > 0 {
			filter.PageSize = pageSize
		}
	}

	orders, total, err := h.service.GetOrders(c.Request.Context(), connectionID, filter)
	if err != nil {
		h.logger.Error("Failed to get orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":   orders,
		"total":    total,
		"page":     filter.Page,
		"pageSize": filter.PageSize,
	})
}

// SyncOrdersRequest represents the request to sync orders
type SyncOrdersRequest struct {
	TimeFrom string `json:"time_from"` // RFC3339 format
	TimeTo   string `json:"time_to"`   // RFC3339 format
}

// SyncOrders manually syncs orders from marketplace
// POST /api/v1/admin/marketplace/connections/:id/orders/sync
func (h *OrderHandler) SyncOrders(c *gin.Context) {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid connection ID"})
		return
	}

	var req SyncOrdersRequest
	_ = c.ShouldBindJSON(&req) // Ignore binding errors, use defaults for empty fields

	// Default to last 7 days if not specified
	var timeFrom, timeTo time.Time
	if req.TimeFrom == "" {
		timeFrom = time.Now().Add(-7 * 24 * time.Hour)
	} else {
		var err error
		timeFrom, err = time.Parse(time.RFC3339, req.TimeFrom)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid time_from format, use RFC3339"})
			return
		}
	}

	if req.TimeTo == "" {
		timeTo = time.Now()
	} else {
		var err error
		timeTo, err = time.Parse(time.RFC3339, req.TimeTo)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid time_to format, use RFC3339"})
			return
		}
	}

	count, err := h.service.SyncOrders(c.Request.Context(), connectionID, timeFrom, timeTo)
	if err != nil {
		h.logger.Error("Failed to sync orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":         err.Error(),
			"orders_synced": count,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Orders synced successfully",
		"orders_synced": count,
		"time_from":     timeFrom,
		"time_to":       timeTo,
	})
}

// UpdateOrderStatusRequest represents the request to update order status
type UpdateOrderStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// UpdateOrderStatus updates order status on the marketplace
// PUT /api/v1/admin/marketplace/connections/:id/orders/:order_id/status
func (h *OrderHandler) UpdateOrderStatus(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("order_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	var req UpdateOrderStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := h.service.UpdateOrderStatus(c.Request.Context(), orderID, req.Status); err != nil {
		h.logger.Error("Failed to update order status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Order status updated"})
}

// GetAWBRequest represents the request to get AWB
type GetAWBRequest struct {
	DocumentType string `json:"document_type"` // NORMAL_AIR_WAYBILL, THERMAL_AIR_WAYBILL
}

// GetAWB gets the AWB download URL for an order
// POST /api/v1/admin/marketplace/connections/:id/orders/:order_id/awb
func (h *OrderHandler) GetAWB(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("order_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	var req GetAWBRequest
	_ = c.ShouldBindJSON(&req) // Optional, use default if not provided

	documentType := req.DocumentType
	if documentType == "" {
		documentType = "NORMAL_AIR_WAYBILL"
	}

	url, err := h.service.GetAWBDownloadURL(c.Request.Context(), orderID, documentType)
	if err != nil {
		h.logger.Error("Failed to get AWB", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"download_url":  url,
		"document_type": documentType,
	})
}
