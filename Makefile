.PHONY: all build test clean run-example fmt vet lint help

# Переменные
BINARY_NAME=echo-server
EXAMPLE_DIR=example
PKG_DIR=pkg/rltcpkit

all: fmt vet build ## Форматирование, проверка и сборка

build: ## Сборка примера echo сервера
	@echo "Building echo server..."
	@cd $(EXAMPLE_DIR) && go build -o $(BINARY_NAME) .
	@echo "Built: $(EXAMPLE_DIR)/$(BINARY_NAME)"

test: ## Запуск тестов
	@echo "Running tests..."
	@go test -v ./$(PKG_DIR)/...

clean: ## Очистка бинарников
	@echo "Cleaning..."
	@rm -f $(EXAMPLE_DIR)/$(BINARY_NAME)
	@rm -f $(EXAMPLE_DIR)/echo-server
	@echo "Cleaned"

run-example: build ## Запуск echo сервера
	@echo "Starting echo server on :8080..."
	@./$(EXAMPLE_DIR)/$(BINARY_NAME) -addr :8080

fmt: ## Форматирование кода
	@echo "Formatting code..."
	@go fmt ./...

vet: ## Статический анализ кода
	@echo "Running go vet..."
	@go vet ./...

lint: ## Запуск линтера (если установлен golangci-lint)
	@echo "Running linter..."
	@which golangci-lint > /dev/null && golangci-lint run || echo "golangci-lint not installed, skipping"

tidy: ## Обновление зависимостей
	@echo "Tidying dependencies..."
	@go mod tidy

install-tools: ## Установка инструментов разработки
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

help: ## Показать эту справку
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
