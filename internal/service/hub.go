package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/golroblox/golrox-realtime/internal/domain"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
)

// Hub maintains active WebSocket clients and broadcasts messages to rooms
type Hub struct {
	// Registered clients by ID
	clients map[string]*domain.Client

	// Room to clients mapping
	rooms map[string]map[string]*domain.Client

	// Register channel for new clients
	register chan *domain.Client

	// Unregister channel for disconnected clients
	unregister chan *domain.Client

	// Shutdown channel
	shutdown chan struct{}

	// Mutex for thread-safe access
	mu sync.RWMutex

	// Logger
	logger *logger.Logger
}

// NewHub creates a new Hub instance
func NewHub(logger *logger.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*domain.Client),
		rooms:      make(map[string]map[string]*domain.Client),
		register:   make(chan *domain.Client),
		unregister: make(chan *domain.Client),
		shutdown:   make(chan struct{}),
		logger:     logger,
	}
}

// Run starts the hub's main loop for handling client registration/unregistration
func (h *Hub) Run() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.shutdown:
			h.logger.Info("Hub shutting down, closing all client connections...")
			h.closeAllClients()
			return

		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case <-ticker.C:
			h.logStats()
		}
	}
}

// Shutdown gracefully stops the hub
func (h *Hub) Shutdown() {
	close(h.shutdown)
}

func (h *Hub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, client := range h.clients {
		close(client.Send)
	}
	h.clients = make(map[string]*domain.Client)
	h.rooms = make(map[string]map[string]*domain.Client)

	h.logger.Infow("All clients disconnected", "count", len(h.clients))
}

// Register adds a new client to the hub (synchronous)
func (h *Hub) Register(client *domain.Client) {
	h.registerClient(client)
}

// RegisterAsync adds a new client to the hub via channel (async)
func (h *Hub) RegisterAsync(client *domain.Client) {
	h.register <- client
}

// Unregister removes a client from the hub
func (h *Hub) Unregister(client *domain.Client) {
	h.unregister <- client
}

func (h *Hub) registerClient(client *domain.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[client.ID] = client

	h.logger.Infow("Client registered",
		"clientId", client.ID,
		"userId", client.GetUserIDString(),
		"anonymous", client.Anonymous,
	)
}

func (h *Hub) unregisterClient(client *domain.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.ID]; ok {
		// Get rooms safely using client's thread-safe method
		rooms := client.GetRooms()

		// Remove client from all rooms
		for _, room := range rooms {
			if clients, ok := h.rooms[room]; ok {
				delete(clients, client.ID)
				if len(clients) == 0 {
					delete(h.rooms, room)
				}
			}
		}

		// Close send channel and remove client
		close(client.Send)
		delete(h.clients, client.ID)

		h.logger.Infow("Client unregistered",
			"clientId", client.ID,
			"userId", client.GetUserIDString(),
		)
	}
}

// JoinRoom adds a client to a room
func (h *Hub) JoinRoom(client *domain.Client, room string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.rooms[room]; !ok {
		h.rooms[room] = make(map[string]*domain.Client)
	}
	h.rooms[room][client.ID] = client
	client.JoinRoom(room)

	h.logger.Debugw("Client joined room",
		"clientId", client.ID,
		"room", room,
	)
}

// LeaveRoom removes a client from a room
func (h *Hub) LeaveRoom(client *domain.Client, room string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.rooms[room]; ok {
		delete(clients, client.ID)
		if len(clients) == 0 {
			delete(h.rooms, room)
		}
	}
	client.LeaveRoom(room)

	h.logger.Debugw("Client left room",
		"clientId", client.ID,
		"room", room,
	)
}

// BroadcastToRoom sends a message to all clients in a room
func (h *Hub) BroadcastToRoom(room string, message *domain.ServerMessage) error {
	h.mu.RLock()
	clients, ok := h.rooms[room]

	if !ok || len(clients) == 0 {
		h.mu.RUnlock()
		return nil // No clients in room
	}

	// Copy clients to avoid holding lock during send
	clientsCopy := make([]*domain.Client, 0, len(clients))
	for _, client := range clients {
		clientsCopy = append(clientsCopy, client)
	}
	h.mu.RUnlock()

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Send to each client with panic protection
	for _, client := range clientsCopy {
		h.safeSend(client, data, room)
	}

	return nil
}

// safeSend sends data to client with panic recovery for closed channels
func (h *Hub) safeSend(client *domain.Client, data []byte, room string) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Warnw("Recovered from send panic (channel closed)",
				"clientId", client.ID,
				"room", room,
			)
		}
	}()

	select {
	case client.Send <- data:
		// Message sent successfully
	default:
		h.logger.Warnw("Client send buffer full",
			"clientId", client.ID,
			"room", room,
		)
	}
}

// SendToClient sends a message to a specific client
func (h *Hub) SendToClient(clientID string, message *domain.ServerMessage) error {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("client not found: %s", clientID)
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return h.safeSendToClient(client, data, clientID)
}

// safeSendToClient sends data to a specific client with panic recovery
func (h *Hub) safeSendToClient(client *domain.Client, data []byte, clientID string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel closed for client: %s", clientID)
			h.logger.Warnw("Recovered from send panic (channel closed)",
				"clientId", clientID,
			)
		}
	}()

	select {
	case client.Send <- data:
		return nil
	default:
		return fmt.Errorf("client send buffer full: %s", clientID)
	}
}

// GetRoomSize returns the number of clients in a room
func (h *Hub) GetRoomSize(room string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.rooms[room]; ok {
		return len(clients)
	}
	return 0
}

// GetConnectedCount returns total connected clients
func (h *Hub) GetConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// GetRoomCount returns total active rooms
func (h *Hub) GetRoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

func (h *Hub) logStats() {
	h.mu.RLock()
	clientCount := len(h.clients)
	roomCount := len(h.rooms)
	h.mu.RUnlock()

	h.logger.Infow("Hub stats",
		"connectedClients", clientCount,
		"activeRooms", roomCount,
	)
}
