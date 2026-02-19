package config

import (
	"log"
	"strings"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	// Application
	AppEnv   string `env:"APP_ENV" envDefault:"development"`
	Port     int    `env:"PORT" envDefault:"3002"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"debug"`

	// RabbitMQ
	RabbitMQURL   string `env:"RABBITMQ_URL,required"`
	RabbitMQQueue string `env:"RABBITMQ_QUEUE" envDefault:"golrox.realtime"`

	// RabbitMQ Exchanges (separated by feature)
	RabbitMQExchangeOrders        string `env:"RABBITMQ_EXCHANGE_ORDERS" envDefault:"golrox.events.orders"`
	RabbitMQExchangePayments      string `env:"RABBITMQ_EXCHANGE_PAYMENTS" envDefault:"golrox.events.payments"`
	RabbitMQExchangeNotifications string `env:"RABBITMQ_EXCHANGE_NOTIFICATIONS" envDefault:"golrox.events.notifications"`
	RabbitMQExchangeChat          string `env:"RABBITMQ_EXCHANGE_CHAT" envDefault:"golrox.events.chat"`

	// JWT (optional - if empty, only anonymous connections allowed)
	JWTSecret string `env:"JWT_SECRET"`

	// CORS
	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
}

// GetExchanges returns all configured exchanges with their routing keys
func (c *Config) GetExchanges() map[string][]string {
	return map[string][]string{
		c.RabbitMQExchangeOrders:        {"order.*"},
		c.RabbitMQExchangePayments:      {"payment.*"},
		c.RabbitMQExchangeNotifications: {"notification.*"},
		c.RabbitMQExchangeChat:          {"chat.outbound"},
	}
}

// Load parses environment variables into Config struct
func Load() Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}
	return cfg
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	return strings.ToLower(c.AppEnv) == "production"
}

// IsDevelopment returns true if running in development environment
func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.AppEnv) == "development"
}
