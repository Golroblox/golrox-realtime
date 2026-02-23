package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/golroblox/golrox-realtime/internal/client"
	"github.com/golroblox/golrox-realtime/internal/config"
	"github.com/golroblox/golrox-realtime/internal/domain"
	"github.com/golroblox/golrox-realtime/internal/middleware"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
	"github.com/golroblox/golrox-realtime/internal/service"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = 25 * time.Second

	// Maximum message size allowed from peer (increased for chat messages + attachment URLs)
	maxMessageSize = 4096
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub           *service.Hub
	authenticator *middleware.Authenticator
	publisher     *client.RabbitMQPublisher
	cfg           *config.Config
	logger        *logger.Logger
	upgrader      websocket.Upgrader
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(
	hub *service.Hub,
	auth *middleware.Authenticator,
	publisher *client.RabbitMQPublisher,
	cfg *config.Config,
	logger *logger.Logger,
) *WebSocketHandler {
	return &WebSocketHandler{
		hub:           hub,
		authenticator: auth,
		publisher:     publisher,
		cfg:           cfg,
		logger:        logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				for _, allowed := range cfg.CORSOrigins {
					if allowed == "*" || allowed == origin {
						return true
					}
				}
				return false
			},
		},
	}
}

// HandleWebSocket handles the WebSocket upgrade and connection
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	// Get token from query param or header
	token := c.Query("token")
	if token == "" {
		token = c.GetHeader("Authorization")
	}

	// Authenticate
	authResult, err := h.authenticator.Authenticate(token)
	if err != nil {
		h.logger.Warnw("Authentication failed",
			"error", err.Error(),
			"clientIP", c.ClientIP(),
		)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Errorw("WebSocket upgrade failed",
			"error", err.Error(),
			"clientIP", c.ClientIP(),
		)
		return
	}

	// Create client
	clientID := uuid.New().String()
	client := domain.NewClient(clientID, conn, authResult.UserID, authResult.Email, authResult.Anonymous)

	// Register client
	h.hub.Register(client)

	// Auto-join user room if authenticated
	if authResult.UserID != nil {
		userRoom := "user:" + *authResult.UserID
		h.hub.JoinRoom(client, userRoom)
	}

	h.logger.Infow("Client connected",
		"clientId", clientID,
		"userId", client.GetUserIDString(),
		"anonymous", authResult.Anonymous,
		"clientIP", c.ClientIP(),
	)

	// Start read and write pumps in goroutines
	go h.writePump(client)
	go h.readPump(client)
}

// readPump handles incoming messages from WebSocket
func (h *WebSocketHandler) readPump(client *domain.Client) {
	defer func() {
		h.hub.Unregister(client)
		client.Conn.Close()
		h.logger.Infow("Client disconnected",
			"clientId", client.ID,
			"userId", client.GetUserIDString(),
		)
	}()

	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Warnw("WebSocket read error",
					"clientId", client.ID,
					"error", err.Error(),
				)
			}
			break
		}

		h.handleClientMessage(client, message)
	}
}

// writePump handles outgoing messages to WebSocket
func (h *WebSocketHandler) writePump(client *domain.Client) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	// Note: Connection is closed by readPump to avoid double-close

	for {
		select {
		case message, ok := <-client.Send:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send each message as its own WebSocket frame to avoid
			// batching multiple JSON objects which breaks JSON.parse on the client
			if err := client.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// Drain remaining queued messages — each as its own frame
			draining := true
			for draining {
				select {
				case msg, ok := <-client.Send:
					if !ok {
						client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
						return
					}
					client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := client.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				default:
					draining = false
				}
			}

		case <-ticker.C:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleClientMessage processes messages from clients
func (h *WebSocketHandler) handleClientMessage(client *domain.Client, message []byte) {
	var msg domain.ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		h.logger.Warnw("Invalid client message",
			"clientId", client.ID,
			"error", err.Error(),
			"message", string(message),
		)
		return
	}

	switch msg.Event {
	case domain.ClientEventSubscribeOrder:
		h.handleSubscribeOrder(client, msg.Data)

	case domain.ClientEventUnsubscribeOrder:
		h.handleUnsubscribeOrder(client, msg.Data)

	case domain.ClientEventPing:
		h.handlePing(client)

	case domain.ClientEventChatSend:
		h.handleChatSend(client, msg.Data)

	default:
		h.logger.Warnw("Unknown client event",
			"clientId", client.ID,
			"event", msg.Event,
		)
	}
}

func (h *WebSocketHandler) handleSubscribeOrder(client *domain.Client, rawData json.RawMessage) {
	orderID := domain.UnmarshalDataAsString(rawData)
	if orderID == "" {
		h.logger.Warnw("Invalid orderId for subscription",
			"clientId", client.ID,
		)
		return
	}

	orderRoom := "order:" + orderID
	h.hub.JoinRoom(client, orderRoom)

	h.logger.Infow("Subscribed to order room",
		"clientId", client.ID,
		"orderId", orderID,
		"room", orderRoom,
	)

	// Send acknowledgment
	response := &domain.ServerMessage{
		Event: domain.SocketEventSubscribedOrder,
		Data: domain.SubscribedOrderPayload{
			OrderID: orderID,
			Success: true,
		},
	}
	if err := h.hub.SendToClient(client.ID, response); err != nil {
		h.logger.Warnw("Failed to send subscription acknowledgment",
			"clientId", client.ID,
			"error", err.Error(),
		)
	}
}

func (h *WebSocketHandler) handleUnsubscribeOrder(client *domain.Client, rawData json.RawMessage) {
	orderID := domain.UnmarshalDataAsString(rawData)
	if orderID == "" {
		return
	}

	orderRoom := "order:" + orderID
	h.hub.LeaveRoom(client, orderRoom)

	h.logger.Debugw("Unsubscribed from order room",
		"clientId", client.ID,
		"orderId", orderID,
	)
}

func (h *WebSocketHandler) handlePing(client *domain.Client) {
	response := &domain.ServerMessage{
		Event: domain.SocketEventPong,
		Data: domain.PongPayload{
			Timestamp: time.Now().UnixMilli(),
		},
	}
	if err := h.hub.SendToClient(client.ID, response); err != nil {
		h.logger.Warnw("Failed to send pong",
			"clientId", client.ID,
			"error", err.Error(),
		)
	}
}

// handleChatSend processes chat:send events from authenticated clients
func (h *WebSocketHandler) handleChatSend(wsClient *domain.Client, rawData json.RawMessage) {
	// Require authenticated user
	if wsClient.UserID == nil {
		h.sendChatError(wsClient, "Authentication required", "AUTH_REQUIRED")
		return
	}
	userID := *wsClient.UserID

	// Parse chat message payload
	var payload domain.ChatSendPayload
	if err := json.Unmarshal(rawData, &payload); err != nil {
		h.sendChatError(wsClient, "Invalid message format", "INVALID_FORMAT")
		return
	}

	// Validate message — allow empty message if attachments are present
	message := strings.TrimSpace(payload.Message)
	hasAttachments := len(payload.AttachmentURLs) > 0
	if message == "" && !hasAttachments {
		h.sendChatError(wsClient, "Message or attachment required", "INVALID_MESSAGE")
		return
	}
	if len(message) > 500 {
		h.sendChatError(wsClient, "Message must be 1-500 characters", "INVALID_MESSAGE")
		return
	}

	correlationID := uuid.New().String()

	// Build RabbitMQ inbound event
	inboundPayload, _ := json.Marshal(domain.ChatInboundPayload{
		UserID:         userID,
		Message:        message,
		AttachmentURLs: payload.AttachmentURLs,
	})

	event := domain.RabbitMQEvent{
		Type:          domain.EventTypeChatInbound,
		Payload:       inboundPayload,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		CorrelationID: correlationID,
	}

	body, _ := json.Marshal(event)

	// Publish to RabbitMQ
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.publisher.Publish(ctx, h.cfg.RabbitMQExchangeChat, "chat.inbound", body); err != nil {
		h.logger.Errorw("Failed to publish chat message",
			"userId", userID,
			"correlationId", correlationID,
			"error", err.Error(),
		)
		h.sendChatError(wsClient, "Failed to send message. Please try again.", "PUBLISH_FAILED")
		return
	}

	h.logger.Infow("Chat message published",
		"userId", userID,
		"correlationId", correlationID,
	)
}

// sendChatError sends a chat:error event to the client
func (h *WebSocketHandler) sendChatError(wsClient *domain.Client, message, code string) {
	errMsg := &domain.ServerMessage{
		Event: domain.SocketEventChatError,
		Data: domain.ChatErrorClientPayload{
			Message: message,
			Code:    code,
		},
	}
	h.hub.SendToClient(wsClient.ID, errMsg)
}
