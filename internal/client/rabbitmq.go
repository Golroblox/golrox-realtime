package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/golroblox/golrox-realtime/internal/config"
	"github.com/golroblox/golrox-realtime/internal/domain"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
	"github.com/golroblox/golrox-realtime/internal/service"
)

const (
	maxReconnectAttempts = 10
	reconnectDelay       = 5 * time.Second
	prefetchCount        = 10
)

// RabbitMQConsumer consumes events from RabbitMQ and dispatches to WebSocket clients
type RabbitMQConsumer struct {
	conn              *amqp.Connection
	channel           *amqp.Channel
	cfg               *config.Config
	logger            *logger.Logger
	dispatcher        *service.EventDispatcher
	mu                sync.Mutex
	isConnected       bool
	reconnectAttempts int
	closed            bool
	done              chan struct{}
}

// NewRabbitMQConsumer creates a new RabbitMQ consumer
func NewRabbitMQConsumer(cfg *config.Config, logger *logger.Logger, dispatcher *service.EventDispatcher) *RabbitMQConsumer {
	return &RabbitMQConsumer{
		cfg:        cfg,
		logger:     logger,
		dispatcher: dispatcher,
		done:       make(chan struct{}),
	}
}

// Connect establishes connection to RabbitMQ
func (c *RabbitMQConsumer) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Infow("Connecting to RabbitMQ...",
		"url", c.maskURL(c.cfg.RabbitMQURL),
	)

	var err error
	c.conn, err = amqp.DialConfig(c.cfg.RabbitMQURL, amqp.Config{
		Heartbeat: 10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	c.channel, err = c.conn.Channel()
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Get all exchanges with their routing keys
	exchanges := c.cfg.GetExchanges()

	// Declare all exchanges
	for exchangeName := range exchanges {
		err = c.channel.ExchangeDeclare(
			exchangeName, // name
			"topic",      // type
			true,         // durable
			false,        // auto-deleted
			false,        // internal
			false,        // no-wait
			nil,          // arguments
		)
		if err != nil {
			c.cleanup()
			return fmt.Errorf("failed to declare exchange %s: %w", exchangeName, err)
		}
		c.logger.Debugw("Exchange declared", "exchange", exchangeName)
	}

	// Declare queue
	queue, err := c.channel.QueueDeclare(
		c.cfg.RabbitMQQueue, // name
		true,                // durable
		false,               // delete when unused
		false,               // exclusive
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind queue to each exchange with appropriate routing keys
	var allRoutingKeys []string
	for exchangeName, routingKeys := range exchanges {
		for _, routingKey := range routingKeys {
			err = c.channel.QueueBind(
				queue.Name,   // queue name
				routingKey,   // routing key
				exchangeName, // exchange
				false,        // no-wait
				nil,          // arguments
			)
			if err != nil {
				c.cleanup()
				return fmt.Errorf("failed to bind queue to %s on %s: %w", routingKey, exchangeName, err)
			}
			c.logger.Debugw("Bound queue to routing key",
				"queue", queue.Name,
				"exchange", exchangeName,
				"routingKey", routingKey,
			)
			allRoutingKeys = append(allRoutingKeys, fmt.Sprintf("%s:%s", exchangeName, routingKey))
		}
	}

	// Set prefetch (QoS)
	err = c.channel.Qos(prefetchCount, 0, false)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Start consuming
	msgs, err := c.channel.Consume(
		queue.Name, // queue
		"",         // consumer tag
		false,      // auto-ack (manual acknowledgment)
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	c.isConnected = true
	c.reconnectAttempts = 0

	// Get exchange names for logging
	var exchangeNames []string
	for name := range exchanges {
		exchangeNames = append(exchangeNames, name)
	}

	c.logger.Infow("RabbitMQ consumer started",
		"exchanges", exchangeNames,
		"queue", queue.Name,
		"bindings", allRoutingKeys,
		"prefetch", prefetchCount,
	)

	// Handle connection errors in goroutine
	go c.handleConnectionErrors()

	// Process messages in goroutine
	go c.processMessages(ctx, msgs)

	return nil
}

func (c *RabbitMQConsumer) processMessages(ctx context.Context, msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Context cancelled, stopping message processing")
			return
		case <-c.done:
			c.logger.Info("Consumer done, stopping message processing")
			return
		case msg, ok := <-msgs:
			if !ok {
				c.logger.Warn("Message channel closed")
				return
			}
			c.handleMessage(msg)
		}
	}
}

func (c *RabbitMQConsumer) handleMessage(msg amqp.Delivery) {
	startTime := time.Now()

	fmt.Printf("\n========== RABBITMQ MESSAGE RECEIVED ==========\n")
	fmt.Printf("RoutingKey: %s\n", msg.RoutingKey)
	fmt.Printf("Body: %s\n", string(msg.Body))
	fmt.Printf("================================================\n\n")

	var event domain.RabbitMQEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		c.logger.Errorw("Failed to unmarshal message",
			"error", err.Error(),
			"routingKey", msg.RoutingKey,
			"body", string(msg.Body),
		)
		// Reject without requeue (send to DLQ if configured)
		msg.Nack(false, false)
		return
	}

	fmt.Printf("Event Type: %s\n", event.Type)
	fmt.Printf("Payload: %s\n", string(event.Payload))

	c.logger.Debugw("Received message",
		"type", event.Type,
		"correlationId", event.CorrelationID,
		"routingKey", msg.RoutingKey,
	)

	// Dispatch event to WebSocket clients
	if err := c.dispatcher.Dispatch(event.Type, event.Payload, event.Timestamp, event.CorrelationID); err != nil {
		c.logger.Errorw("Failed to dispatch event",
			"error", err.Error(),
			"type", event.Type,
			"correlationId", event.CorrelationID,
		)
		// Reject without requeue
		msg.Nack(false, false)
		return
	}

	// Acknowledge successful processing
	msg.Ack(false)

	duration := time.Since(startTime)
	c.logger.Infow("Message processed successfully",
		"type", event.Type,
		"correlationId", event.CorrelationID,
		"duration", duration.String(),
	)
}

func (c *RabbitMQConsumer) handleConnectionErrors() {
	notifyClose := c.conn.NotifyClose(make(chan *amqp.Error))

	for {
		select {
		case <-c.done:
			return
		case err := <-notifyClose:
			if err == nil {
				return // Normal close
			}

			c.logger.Warnw("RabbitMQ connection lost",
				"error", err.Error(),
			)

			c.mu.Lock()
			c.isConnected = false
			c.mu.Unlock()

			c.attemptReconnect()
			return
		}
	}
}

func (c *RabbitMQConsumer) attemptReconnect() {
	for i := 0; i < maxReconnectAttempts; i++ {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.reconnectAttempts = i + 1
		c.mu.Unlock()

		c.logger.Warnw("Attempting to reconnect to RabbitMQ",
			"attempt", i+1,
			"maxAttempts", maxReconnectAttempts,
		)

		time.Sleep(reconnectDelay)

		if err := c.Connect(context.Background()); err != nil {
			c.logger.Warnw("Reconnect attempt failed",
				"attempt", i+1,
				"error", err.Error(),
			)
			continue
		}

		c.logger.Info("Reconnected to RabbitMQ successfully")
		return
	}

	c.logger.Error("Max reconnect attempts reached, giving up")
}

// Disconnect gracefully closes the RabbitMQ connection
func (c *RabbitMQConsumer) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	c.isConnected = false
	close(c.done)

	c.cleanup()
	c.logger.Info("Disconnected from RabbitMQ")
}

func (c *RabbitMQConsumer) cleanup() {
	if c.channel != nil {
		c.channel.Close()
		c.channel = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// IsConnected returns the current connection status
func (c *RabbitMQConsumer) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isConnected
}

// maskURL masks the password in the RabbitMQ URL for logging
func (c *RabbitMQConsumer) maskURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "invalid-url"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword(parsed.User.Username(), "****")
	}
	return parsed.String()
}
