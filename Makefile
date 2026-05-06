.PHONY: all build run test clean

APP_NAME=dp
BIN_DIR=bin
CMD_DIR=cmd/disguised-penguin

all: build

build:
	@echo "Building $(APP_NAME)..."
	@go build -ldflags "-X disguised-penguin/internal/cli.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o $(BIN_DIR)/$(APP_NAME) ./$(CMD_DIR)

run: build
	@echo "Running $(APP_NAME)..."
	@./$(BIN_DIR)/$(APP_NAME)

test:
	@echo "Running tests..."
	@go test -v ./...

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR)
