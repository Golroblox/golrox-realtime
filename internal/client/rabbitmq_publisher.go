package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/golroblox/golrox-realtime/internal/config"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
)

const (
	publishTimeout         = 5 * time.Second
	publisherReconnectMax  = 10
	publisherReconnectWait = 5 * time.Second
)

// RabbitMQPublisher publishes messages to RabbitMQ exchanges.
// Uses a separate connection from the consumer following AMQP best practices.
type RabbitMQPublisher struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	cfg         *config.Config
	logger      *logger.Logger
	mu          sync.Mutex
	isConnected bool
	closed      bool
	done        chan struct{}
}

// NewRabbitMQPublisher creates a new RabbitMQ publisher
func NewRabbitMQPublisher(cfg *config.Config, logger *logger.Logger) *RabbitMQPublisher {
	return &RabbitMQPublisher{
		cfg:    cfg,
		logger: logger,
		done:   make(chan struct{}),
	}
}

// Connect establishes a connection and declares the chat exchange
func (p *RabbitMQPublisher) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Info("Connecting RabbitMQ publisher...")

	var err error
	p.conn, err = amqp.Dial(p.cfg.RabbitMQURL)
	if err != nil {
		return fmt.Errorf("publisher: failed to connect to RabbitMQ: %w", err)
	}

	p.channel, err = p.conn.Channel()
	if err != nil {
		p.conn.Close()
		return fmt.Errorf("publisher: failed to open channel: %w", err)
	}

	// Declare the chat exchange
	err = p.channel.ExchangeDeclare(
		p.cfg.RabbitMQExchangeChat, // name
		"topic",                    // type
		true,                       // durable
		false,                      // auto-deleted
		false,                      // internal
		false,                      // no-wait
		nil,                        // arguments
	)
	if err != nil {
		p.cleanup()
		return fmt.Errorf("publisher: failed to declare exchange %s: %w", p.cfg.RabbitMQExchangeChat, err)
	}

	// Enable publisher confirms for reliability
	if err = p.channel.Confirm(false); err != nil {
		p.cleanup()
		return fmt.Errorf("publisher: failed to enable confirms: %w", err)
	}

	p.isConnected = true

	p.logger.Infow("RabbitMQ publisher connected",
		"exchange", p.cfg.RabbitMQExchangeChat,
	)

	go p.handleConnectionErrors()

	return nil
}

// Publish sends a message to the specified exchange with the given routing key
func (p *RabbitMQPublisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	p.mu.Lock()
	if !p.isConnected || p.channel == nil {
		p.mu.Unlock()
		return fmt.Errorf("publisher: not connected")
	}
	ch := p.channel
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	return ch.PublishWithContext(ctx,
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
}

// IsConnected returns the current connection status
func (p *RabbitMQPublisher) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isConnected
}

// Disconnect gracefully closes the publisher connection
func (p *RabbitMQPublisher) Disconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	p.closed = true
	p.isConnected = false
	close(p.done)

	p.cleanup()
	p.logger.Info("RabbitMQ publisher disconnected")
}

func (p *RabbitMQPublisher) cleanup() {
	if p.channel != nil {
		p.channel.Close()
		p.channel = nil
	}
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
}

func (p *RabbitMQPublisher) handleConnectionErrors() {
	notifyClose := p.conn.NotifyClose(make(chan *amqp.Error))

	for {
		select {
		case <-p.done:
			return
		case err := <-notifyClose:
			if err == nil {
				return
			}

			p.logger.Warnw("RabbitMQ publisher connection lost",
				"error", err.Error(),
			)

			p.mu.Lock()
			p.isConnected = false
			p.mu.Unlock()

			p.attemptReconnect()
			return
		}
	}
}

func (p *RabbitMQPublisher) attemptReconnect() {
	for i := 0; i < publisherReconnectMax; i++ {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		p.logger.Warnw("Publisher attempting reconnect",
			"attempt", i+1,
			"maxAttempts", publisherReconnectMax,
		)

		time.Sleep(publisherReconnectWait)

		if err := p.Connect(context.Background()); err != nil {
			p.logger.Warnw("Publisher reconnect failed",
				"attempt", i+1,
				"error", err.Error(),
			)
			continue
		}

		p.logger.Info("Publisher reconnected successfully")
		return
	}

	p.logger.Error("Publisher max reconnect attempts reached, giving up")
}
