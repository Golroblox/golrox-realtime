package domain

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a WebSocket connection
type Client struct {
	// Unique client identifier
	ID string

	// WebSocket connection
	Conn *websocket.Conn

	// User ID (nil for anonymous connections)
	UserID *string

	// User email (nil for anonymous connections)
	Email *string

	// Whether this is an anonymous connection
	Anonymous bool

	// Room memberships (room name -> bool)
	Rooms map[string]bool

	// Channel for outbound messages
	Send chan []byte

	// Mutex for thread-safe room operations
	mu sync.RWMutex

	// Connection timestamp
	CreatedAt time.Time
}

// NewClient creates a new WebSocket client
func NewClient(id string, conn *websocket.Conn, userID, email *string, anonymous bool) *Client {
	return &Client{
		ID:        id,
		Conn:      conn,
		UserID:    userID,
		Email:     email,
		Anonymous: anonymous,
		Rooms:     make(map[string]bool),
		Send:      make(chan []byte, 1024),
		CreatedAt: time.Now(),
	}
}

// JoinRoom adds the client to a room
func (c *Client) JoinRoom(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Rooms[room] = true
}

// LeaveRoom removes the client from a room
func (c *Client) LeaveRoom(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Rooms, room)
}

// InRoom checks if the client is in a specific room
func (c *Client) InRoom(room string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Rooms[room]
}

// GetRooms returns all rooms the client is in
func (c *Client) GetRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rooms := make([]string, 0, len(c.Rooms))
	for room := range c.Rooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// GetUserIDString returns the user ID as string or empty if nil
func (c *Client) GetUserIDString() string {
	if c.UserID != nil {
		return *c.UserID
	}
	return ""
}

// GetEmailString returns the email as string or empty if nil
func (c *Client) GetEmailString() string {
	if c.Email != nil {
		return *c.Email
	}
	return ""
}
