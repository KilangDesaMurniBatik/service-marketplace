package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Event subjects
const (
	SubjectInventoryStockChanged = "inventory.stock.changed"
	SubjectMarketplaceSyncOK     = "marketplace.sync.completed"
	SubjectMarketplaceSyncFailed = "marketplace.sync.failed"

	// Catalog events - subscribe to product changes for auto-sync
	SubjectProductCreated = "product.created"
	SubjectProductUpdated = "product.updated"
	SubjectProductDeleted = "product.deleted"
)

// StockChangedEvent represents an inventory change event
type StockChangedEvent struct {
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	SKU         string     `json:"sku"`
	OldQuantity int        `json:"old_quantity"`
	NewQuantity int        `json:"new_quantity"`
	WarehouseID string     `json:"warehouse_id,omitempty"`
	Reason      string     `json:"reason"` // sale, adjustment, return, etc.
	Timestamp   time.Time  `json:"timestamp"`
}

// ProductUpdatedEvent represents a product update from catalog service
type ProductUpdatedEvent struct {
	ProductID  string    `json:"product_id"`
	SKU        string    `json:"sku"`
	Name       string    `json:"name"`
	CategoryID string    `json:"category_id"`
	BasePrice  float64   `json:"base_price"`
	IsActive   bool      `json:"is_active"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ProductDeletedEvent represents a product deletion from catalog service
type ProductDeletedEvent struct {
	ProductID string    `json:"product_id"`
	DeletedAt time.Time `json:"deleted_at"`
}

// SyncCompletedEvent represents a successful sync event
type SyncCompletedEvent struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	Platform     string    `json:"platform"`
	ProductID    uuid.UUID `json:"product_id"`
	SyncType     string    `json:"sync_type"` // inventory, product, order
	Timestamp    time.Time `json:"timestamp"`
}

// SyncFailedEvent represents a failed sync event
type SyncFailedEvent struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	Platform     string    `json:"platform"`
	ProductID    uuid.UUID `json:"product_id"`
	SyncType     string    `json:"sync_type"`
	Error        string    `json:"error"`
	Timestamp    time.Time `json:"timestamp"`
}

// Subscriber handles NATS event subscriptions
type Subscriber struct {
	nc      *nats.Conn
	logger  *zap.Logger
	handler EventHandler
	subs    []*nats.Subscription
}

// EventHandler defines the interface for handling events
type EventHandler interface {
	HandleStockChanged(event *StockChangedEvent) error
	HandleProductUpdated(event *ProductUpdatedEvent) error
	HandleProductDeleted(event *ProductDeletedEvent) error
}

// NewSubscriber creates a new NATS subscriber
func NewSubscriber(nc *nats.Conn, handler EventHandler, logger *zap.Logger) *Subscriber {
	return &Subscriber{
		nc:      nc,
		logger:  logger,
		handler: handler,
		subs:    make([]*nats.Subscription, 0),
	}
}

// Start subscribes to all relevant events
func (s *Subscriber) Start() error {
	// Subscribe to inventory changes
	sub, err := s.nc.Subscribe(SubjectInventoryStockChanged, s.handleStockChanged)
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	s.logger.Info("Subscribed to event", zap.String("subject", SubjectInventoryStockChanged))

	// Subscribe to product updates for auto-sync to marketplaces
	sub, err = s.nc.Subscribe(SubjectProductUpdated, s.handleProductUpdated)
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	s.logger.Info("Subscribed to event", zap.String("subject", SubjectProductUpdated))

	// Subscribe to product deletions
	sub, err = s.nc.Subscribe(SubjectProductDeleted, s.handleProductDeleted)
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	s.logger.Info("Subscribed to event", zap.String("subject", SubjectProductDeleted))

	s.logger.Info("NATS subscriber started with all subscriptions")
	return nil
}

// Stop unsubscribes from all events
func (s *Subscriber) Stop() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
	s.logger.Info("NATS subscriber stopped")
}

// handleStockChanged processes stock changed events
func (s *Subscriber) handleStockChanged(msg *nats.Msg) {
	var event StockChangedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("Failed to unmarshal stock changed event", zap.Error(err))
		return
	}

	s.logger.Info("Received stock changed event",
		zap.String("product_id", event.ProductID.String()),
		zap.Int("new_quantity", event.NewQuantity),
	)

	if err := s.handler.HandleStockChanged(&event); err != nil {
		s.logger.Error("Failed to handle stock changed event", zap.Error(err))
	}
}

// handleProductUpdated processes product updated events
func (s *Subscriber) handleProductUpdated(msg *nats.Msg) {
	var event ProductUpdatedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("Failed to unmarshal product updated event", zap.Error(err))
		return
	}

	s.logger.Info("Received product updated event",
		zap.String("product_id", event.ProductID),
		zap.String("name", event.Name),
		zap.Float64("base_price", event.BasePrice),
	)

	if err := s.handler.HandleProductUpdated(&event); err != nil {
		s.logger.Error("Failed to handle product updated event",
			zap.String("product_id", event.ProductID),
			zap.Error(err),
		)
	}
}

// handleProductDeleted processes product deleted events
func (s *Subscriber) handleProductDeleted(msg *nats.Msg) {
	var event ProductDeletedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("Failed to unmarshal product deleted event", zap.Error(err))
		return
	}

	s.logger.Info("Received product deleted event",
		zap.String("product_id", event.ProductID),
	)

	if err := s.handler.HandleProductDeleted(&event); err != nil {
		s.logger.Error("Failed to handle product deleted event",
			zap.String("product_id", event.ProductID),
			zap.Error(err),
		)
	}
}

// Publisher handles publishing events to NATS
type Publisher struct {
	nc     *nats.Conn
	logger *zap.Logger
}

// NewPublisher creates a new NATS publisher
func NewPublisher(nc *nats.Conn, logger *zap.Logger) *Publisher {
	return &Publisher{nc: nc, logger: logger}
}

// PublishSyncCompleted publishes a sync completed event
func (p *Publisher) PublishSyncCompleted(event *SyncCompletedEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.nc.Publish(SubjectMarketplaceSyncOK, data)
}

// PublishSyncFailed publishes a sync failed event
func (p *Publisher) PublishSyncFailed(event *SyncFailedEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.nc.Publish(SubjectMarketplaceSyncFailed, data)
}
