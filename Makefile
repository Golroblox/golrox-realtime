.PHONY: dev build run clean test tidy install-air docker-build docker-run

# Development with hot reload
dev:
	@if command -v air > /dev/null; then \
		air; \
	else \
		echo "air not installed. Run 'make install-air' first."; \
		exit 1; \
	fi

# Build binary
build:
	@echo "Building..."
	@go build -ldflags="-w -s" -o bin/golrox-realtime ./cmd/main.go

# Run without hot reload
run:
	@go run ./cmd/main.go

# Clean build artifacts
clean:
	@rm -rf bin/
	@rm -rf tmp/

# Run tests
test:
	@go test -v ./...

# Run tests with coverage
test-cover:
	@go test -v -cover ./...

# Tidy dependencies
tidy:
	@go mod tidy

# Download dependencies
deps:
	@go mod download

# Install air for hot reload
install-air:
	@go install github.com/air-verse/air@latest

# Build Docker image
docker-build:
	@docker build -t golrox-realtime .

# Run Docker container
docker-run:
	@docker run -p 3002:3002 --env-file .env golrox-realtime

# Format code
fmt:
	@go fmt ./...

# Lint code
lint:
	@golangci-lint run

# Check for vulnerabilities
vuln:
	@govulncheck ./...
