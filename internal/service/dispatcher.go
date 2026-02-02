package service

import (
	"encoding/json"
	"fmt"

	"github.com/golroblox/golrox-realtime/internal/domain"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
)

// EventDispatcher routes RabbitMQ events to WebSocket rooms
type EventDispatcher struct {
	hub    *Hub
	logger *logger.Logger
}

// NewEventDispatcher creates a new event dispatcher
func NewEventDispatcher(hub *Hub, logger *logger.Logger) *EventDispatcher {
	return &EventDispatcher{
		hub:    hub,
		logger: logger,
	}
}

// Dispatch routes an event to appropriate rooms based on event type
func (d *EventDispatcher) Dispatch(eventType string, rawPayload json.RawMessage, timestamp, correlationID string) error {
	d.logger.Debugw("Dispatching event",
		"type", eventType,
		"correlationId", correlationID,
	)

	switch eventType {
	case domain.EventTypePaymentSuccess:
		return d.handlePaymentSuccess(rawPayload, timestamp, correlationID)
	case domain.EventTypePaymentExpired:
		return d.handlePaymentExpired(rawPayload, timestamp, correlationID)
	case domain.EventTypeOrderProcessing:
		return d.handleOrderProcessing(rawPayload, timestamp, correlationID)
	case domain.EventTypeOrderDelivered:
		return d.handleOrderDelivered(rawPayload, timestamp, correlationID)
	case domain.EventTypeOrderFailed:
		return d.handleOrderFailed(rawPayload, timestamp, correlationID)
	case domain.EventTypeNotificationNew:
		return d.handleNotificationNew(rawPayload, timestamp, correlationID)
	default:
		d.logger.Warnw("Unknown event type received",
			"type", eventType,
			"correlationId", correlationID,
		)
		return nil
	}
}

func (d *EventDispatcher) handlePaymentSuccess(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.PaymentSuccessPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payment.success payload: %w", err)
	}

	clientPayload := domain.PaymentSuccessClientPayload{
		OrderID:       payload.OrderID,
		Amount:        payload.Amount,
		PaymentMethod: payload.PaymentMethod,
		Timestamp:     timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventPaymentSuccess,
		Data:  clientPayload,
	}

	// Emit to order room
	orderRoom := "order:" + payload.OrderID
	if err := d.hub.BroadcastToRoom(orderRoom, message); err != nil {
		d.logger.Errorw("Failed to broadcast to order room",
			"room", orderRoom,
			"error", err.Error(),
		)
	}

	// Emit to user room if buyerId present
	if payload.BuyerID != "" {
		userRoom := "user:" + payload.BuyerID
		if err := d.hub.BroadcastToRoom(userRoom, message); err != nil {
			d.logger.Errorw("Failed to broadcast to user room",
				"room", userRoom,
				"error", err.Error(),
			)
		}
	}

	d.logger.Infow("Dispatched payment.success",
		"orderId", payload.OrderID,
		"buyerId", payload.BuyerID,
		"orderRoomSize", d.hub.GetRoomSize(orderRoom),
		"correlationId", correlationID,
	)

	return nil
}

func (d *EventDispatcher) handlePaymentExpired(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.PaymentExpiredPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payment.expired payload: %w", err)
	}

	clientPayload := domain.PaymentExpiredClientPayload{
		OrderID:   payload.OrderID,
		Timestamp: timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventPaymentExpired,
		Data:  clientPayload,
	}

	// Emit to order room
	orderRoom := "order:" + payload.OrderID
	d.hub.BroadcastToRoom(orderRoom, message)

	// Emit to user room if buyerId present
	if payload.BuyerID != "" {
		userRoom := "user:" + payload.BuyerID
		d.hub.BroadcastToRoom(userRoom, message)
	}

	d.logger.Infow("Dispatched payment.expired",
		"orderId", payload.OrderID,
		"buyerId", payload.BuyerID,
		"correlationId", correlationID,
	)

	return nil
}

func (d *EventDispatcher) handleOrderProcessing(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.OrderProcessingPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal order.processing payload: %w", err)
	}

	clientPayload := domain.OrderProcessingClientPayload{
		OrderID:     payload.OrderID,
		OrderItemID: payload.OrderItemID,
		Timestamp:   timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventOrderProcessing,
		Data:  clientPayload,
	}

	// Emit to order room
	orderRoom := "order:" + payload.OrderID
	d.hub.BroadcastToRoom(orderRoom, message)

	// Emit to user room if buyerId present
	if payload.BuyerID != "" {
		userRoom := "user:" + payload.BuyerID
		d.hub.BroadcastToRoom(userRoom, message)
	}

	d.logger.Infow("Dispatched order.processing",
		"orderId", payload.OrderID,
		"orderItemId", payload.OrderItemID,
		"orderRoomSize", d.hub.GetRoomSize(orderRoom),
		"correlationId", correlationID,
	)

	return nil
}

func (d *EventDispatcher) handleOrderDelivered(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.OrderDeliveredPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal order.delivered payload: %w", err)
	}

	clientPayload := domain.OrderDeliveredClientPayload{
		OrderID:     payload.OrderID,
		OrderItemID: payload.OrderItemID,
		Timestamp:   timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventOrderDelivered,
		Data:  clientPayload,
	}

	// Emit to order room
	orderRoom := "order:" + payload.OrderID
	d.hub.BroadcastToRoom(orderRoom, message)

	// Emit to user room if buyerId present
	if payload.BuyerID != "" {
		userRoom := "user:" + payload.BuyerID
		d.hub.BroadcastToRoom(userRoom, message)
	}

	d.logger.Infow("Dispatched order.delivered",
		"orderId", payload.OrderID,
		"orderItemId", payload.OrderItemID,
		"orderRoomSize", d.hub.GetRoomSize(orderRoom),
		"correlationId", correlationID,
	)

	return nil
}

func (d *EventDispatcher) handleOrderFailed(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.OrderFailedPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal order.failed payload: %w", err)
	}

	clientPayload := domain.OrderFailedClientPayload{
		OrderID:     payload.OrderID,
		OrderItemID: payload.OrderItemID,
		Reason:      payload.Reason,
		Timestamp:   timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventOrderFailed,
		Data:  clientPayload,
	}

	// Emit to order room
	orderRoom := "order:" + payload.OrderID
	d.hub.BroadcastToRoom(orderRoom, message)

	// Emit to user room if buyerId present
	if payload.BuyerID != "" {
		userRoom := "user:" + payload.BuyerID
		d.hub.BroadcastToRoom(userRoom, message)
	}

	d.logger.Infow("Dispatched order.failed",
		"orderId", payload.OrderID,
		"orderItemId", payload.OrderItemID,
		"reason", payload.Reason,
		"orderRoomSize", d.hub.GetRoomSize(orderRoom),
		"correlationId", correlationID,
	)

	return nil
}

func (d *EventDispatcher) handleNotificationNew(rawPayload json.RawMessage, timestamp, correlationID string) error {
	var payload domain.NotificationNewPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal notification.new payload: %w", err)
	}

	clientPayload := domain.NotificationNewClientPayload{
		NotificationID: payload.NotificationID,
		Title:          payload.Title,
		Message:        payload.Message,
		Type:           payload.Type,
		Timestamp:      timestamp,
	}

	message := &domain.ServerMessage{
		Event: domain.SocketEventNotificationNew,
		Data:  clientPayload,
	}

	// Only emit to user room
	userRoom := "user:" + payload.UserID
	d.hub.BroadcastToRoom(userRoom, message)

	d.logger.Infow("Dispatched notification.new",
		"userId", payload.UserID,
		"notificationId", payload.NotificationID,
		"correlationId", correlationID,
	)

	return nil
}
