package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golroblox/golrox-realtime/internal/client"
	"github.com/golroblox/golrox-realtime/internal/service"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	hub      *service.Hub
	consumer *client.RabbitMQConsumer
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(hub *service.Hub, consumer *client.RabbitMQConsumer) *HealthHandler {
	return &HealthHandler{
		hub:      hub,
		consumer: consumer,
	}
}

// Health returns the service health status
func (h *HealthHandler) Health(c *gin.Context) {
	rabbitmqStatus := "disconnected"
	if h.consumer != nil && h.consumer.IsConnected() {
		rabbitmqStatus = "connected"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           "ok",
		"service":          "golrox-realtime",
		"connectedClients": h.hub.GetConnectedCount(),
		"activeRooms":      h.hub.GetRoomCount(),
		"rabbitmq":         rabbitmqStatus,
	})
}
