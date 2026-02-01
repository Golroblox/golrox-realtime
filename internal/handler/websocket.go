package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

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

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub           *service.Hub
	authenticator *middleware.Authenticator
	logger        *logger.Logger
	upgrader      websocket.Upgrader
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(
	hub *service.Hub,
	auth *middleware.Authenticator,
	cfg *config.Config,
	logger *logger.Logger,
) *WebSocketHandler {
	return &WebSocketHandler{
		hub:           hub,
		authenticator: auth,
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

			w, err := client.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Drain queued messages to reduce syscalls
			n := len(client.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-client.Send)
			}

			if err := w.Close(); err != nil {
				return
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

	default:
		h.logger.Warnw("Unknown client event",
			"clientId", client.ID,
			"event", msg.Event,
		)
	}
}

func (h *WebSocketHandler) handleSubscribeOrder(client *domain.Client, orderID string) {
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

func (h *WebSocketHandler) handleUnsubscribeOrder(client *domain.Client, orderID string) {
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
