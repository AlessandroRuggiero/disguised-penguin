.PHONY: all build run test clean dist dist-linux-amd64 dist-darwin-amd64 dist-darwin-arm64

APP_NAME=dp
BIN_DIR=bin
CMD_DIR=cmd/disguised-penguin
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-X disguised-penguin/internal/cli.Version=$(VERSION)

all: build

build:
	@echo "Building $(APP_NAME)..."
	@go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) ./$(CMD_DIR)

run: build
	@echo "Running $(APP_NAME)..."
	@./$(BIN_DIR)/$(APP_NAME)

test:
	@echo "Running tests..."
	@go test -v ./...

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR)

# Cross-compiled release binaries. All pure Go (no CGO), so these build fine
# from any host without needing a matching runner per OS/arch.
dist: dist-linux-amd64 dist-darwin-amd64 dist-darwin-arm64

dist-linux-amd64:
	@echo "Building $(APP_NAME) for linux/amd64..."
	@GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 ./$(CMD_DIR)

dist-darwin-amd64:
	@echo "Building $(APP_NAME) for darwin/amd64..."
	@GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-amd64 ./$(CMD_DIR)

dist-darwin-arm64:
	@echo "Building $(APP_NAME) for darwin/arm64..."
	@GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-arm64 ./$(CMD_DIR)
