package domain

import "encoding/json"

// ==========================================
// RabbitMQ Event Types (routing keys)
// ==========================================

const (
	EventTypePaymentSuccess   = "payment.success"
	EventTypePaymentExpired   = "payment.expired"
	EventTypeOrderProcessing  = "order.processing"
	EventTypeOrderDelivered   = "order.delivered"
	EventTypeOrderFailed      = "order.failed"
	EventTypeNotificationNew  = "notification.new"
)

// ==========================================
// Socket Event Names (emitted to clients)
// ==========================================

const (
	SocketEventPaymentSuccess   = "payment:success"
	SocketEventPaymentExpired   = "payment:expired"
	SocketEventOrderProcessing  = "order:processing"
	SocketEventOrderDelivered   = "order:delivered"
	SocketEventOrderFailed      = "order:failed"
	SocketEventNotificationNew  = "notification:new"
	SocketEventSubscribedOrder  = "subscribed:order"
	SocketEventPong             = "pong"
)

// ==========================================
// Client Event Names (received from clients)
// ==========================================

const (
	ClientEventSubscribeOrder   = "subscribe:order"
	ClientEventUnsubscribeOrder = "unsubscribe:order"
	ClientEventPing             = "ping"
)

// ==========================================
// RabbitMQ Event Structure
// ==========================================

// RabbitMQEvent represents the incoming event structure from RabbitMQ
type RabbitMQEvent struct {
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	Timestamp     string          `json:"timestamp"`
	CorrelationID string          `json:"correlationId"`
}

// ==========================================
// RabbitMQ Event Payloads
// ==========================================

// PaymentSuccessPayload represents payment.success event payload
type PaymentSuccessPayload struct {
	OrderID       string `json:"orderId"`
	BuyerID       string `json:"buyerId"`
	Amount        int    `json:"amount"`
	PaymentMethod string `json:"paymentMethod"`
}

// PaymentExpiredPayload represents payment.expired event payload
type PaymentExpiredPayload struct {
	OrderID string `json:"orderId"`
	BuyerID string `json:"buyerId"`
}

// OrderProcessingPayload represents order.processing event payload
type OrderProcessingPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	BuyerID     string `json:"buyerId"`
}

// OrderDeliveredPayload represents order.delivered event payload
type OrderDeliveredPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	BuyerID     string `json:"buyerId"`
}

// OrderFailedPayload represents order.failed event payload
type OrderFailedPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	BuyerID     string `json:"buyerId"`
	Reason      string `json:"reason"`
}

// NotificationNewPayload represents notification.new event payload
type NotificationNewPayload struct {
	NotificationID string `json:"notificationId"`
	UserID         string `json:"userId"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Type           string `json:"type"`
}

// ==========================================
// Client Message Structures
// ==========================================

// ClientMessage represents a message from WebSocket client
type ClientMessage struct {
	Event string `json:"event"`
	Data  string `json:"data,omitempty"`
}

// ServerMessage represents a message to WebSocket client
type ServerMessage struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// ==========================================
// Client Payloads (for outgoing messages)
// These remove sensitive fields like buyerId
// ==========================================

// PaymentSuccessClientPayload is sent to clients (removes buyerId)
type PaymentSuccessClientPayload struct {
	OrderID       string `json:"orderId"`
	Amount        int    `json:"amount"`
	PaymentMethod string `json:"paymentMethod"`
	Timestamp     string `json:"timestamp"`
}

// PaymentExpiredClientPayload is sent to clients
type PaymentExpiredClientPayload struct {
	OrderID   string `json:"orderId"`
	Timestamp string `json:"timestamp"`
}

// OrderProcessingClientPayload is sent to clients
type OrderProcessingClientPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	Timestamp   string `json:"timestamp"`
}

// OrderDeliveredClientPayload is sent to clients
type OrderDeliveredClientPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	Timestamp   string `json:"timestamp"`
}

// OrderFailedClientPayload is sent to clients
type OrderFailedClientPayload struct {
	OrderID     string `json:"orderId"`
	OrderItemID string `json:"orderItemId"`
	Reason      string `json:"reason"`
	Timestamp   string `json:"timestamp"`
}

// NotificationNewClientPayload is sent to clients
type NotificationNewClientPayload struct {
	NotificationID string `json:"notificationId"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Type           string `json:"type"`
	Timestamp      string `json:"timestamp"`
}

// ==========================================
// Response Payloads
// ==========================================

// SubscribedOrderPayload for subscription acknowledgment
type SubscribedOrderPayload struct {
	OrderID string `json:"orderId"`
	Success bool   `json:"success"`
}

// PongPayload for ping response
type PongPayload struct {
	Timestamp int64 `json:"timestamp"`
}
