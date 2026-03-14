.PHONY: all build run dev test clean docker docker-up docker-down docker-logs deps lint fmt help

# Variables
APP_NAME := real-estayer
GO := go
GOFLAGS := -v
BINARY := bin/server
DOCKER_COMPOSE := docker compose

# Colors
GREEN  := \033[0;32m
YELLOW := \033[0;33m
BLUE   := \033[0;34m
NC     := \033[0m

## help: Display this help message
help:
	@echo "$(BLUE)Real-Estayer V3 - Makefile Commands$(NC)"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## all: Build and run
all: build run

## deps: Download dependencies
deps:
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	$(GO) mod download
	$(GO) mod tidy

## build: Build the application
build:
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/server

## run: Run the application locally
run: build
	@echo "$(GREEN)Running $(APP_NAME)...$(NC)"
	./$(BINARY)

## dev: Run with live reload (requires air)
dev:
	@echo "$(GREEN)Starting development server with live reload...$(NC)"
	@if command -v air > /dev/null; then \
		air; \
	else \
		echo "$(YELLOW)Air not found. Install with: go install github.com/air-verse/air@latest$(NC)"; \
		echo "$(YELLOW)Falling back to regular run...$(NC)"; \
		$(GO) run ./cmd/server; \
	fi

## test: Run tests
test:
	@echo "$(GREEN)Running tests...$(NC)"
	$(GO) test -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

## lint: Run linter (requires golangci-lint)
lint:
	@echo "$(GREEN)Running linter...$(NC)"
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "$(YELLOW)golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(NC)"; \
	fi

## fmt: Format code
fmt:
	@echo "$(GREEN)Formatting code...$(NC)"
	$(GO) fmt ./...
	@if command -v goimports > /dev/null; then \
		goimports -w .; \
	fi

## clean: Clean build artifacts
clean:
	@echo "$(GREEN)Cleaning...$(NC)"
	rm -rf bin/
	rm -f coverage.out coverage.html
	$(GO) clean

## docker: Build Docker image
docker:
	@echo "$(GREEN)Building Docker image...$(NC)"
	docker build -t $(APP_NAME):latest .

## docker-up: Start all services with Docker Compose
docker-up:
	@echo "$(GREEN)Starting services...$(NC)"
	$(DOCKER_COMPOSE) up -d

## docker-up-build: Rebuild and start all services
docker-up-build:
	@echo "$(GREEN)Rebuilding and starting services...$(NC)"
	$(DOCKER_COMPOSE) up -d --build

## docker-down: Stop all services
docker-down:
	@echo "$(GREEN)Stopping services...$(NC)"
	$(DOCKER_COMPOSE) down

## docker-logs: View logs from all services
docker-logs:
	$(DOCKER_COMPOSE) logs -f

## docker-logs-app: View logs from app service only
docker-logs-app:
	$(DOCKER_COMPOSE) logs -f app

## docker-clean: Stop services and remove volumes
docker-clean:
	@echo "$(YELLOW)Stopping services and removing volumes...$(NC)"
	$(DOCKER_COMPOSE) down -v

## docker-admin: Start with admin tools (mongo-express)
docker-admin:
	@echo "$(GREEN)Starting services with admin tools...$(NC)"
	$(DOCKER_COMPOSE) --profile admin up -d

## mongo-shell: Open MongoDB shell
mongo-shell:
	docker exec -it $$(docker ps -qf "name=mongodb") mongosh realestayer

## redis-cli: Open Redis CLI
redis-cli:
	docker exec -it $$(docker ps -qf "name=redis") redis-cli

## env: Create .env file from example
env:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "$(GREEN).env file created. Please update with your values.$(NC)"; \
	else \
		echo "$(YELLOW).env file already exists.$(NC)"; \
	fi

## seed: Seed the database with sample data
seed:
	@echo "$(GREEN)Seeding database...$(NC)"
	$(GO) run ./cmd/seed

# Default target
.DEFAULT_GOAL := help
