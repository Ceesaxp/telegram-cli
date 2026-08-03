.PHONY: build run test lint clean

BINARY_NAME=tele-tui
BUILD_DIR=bin

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/teletui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/telegram-mcp ./cmd/telegram-mcp
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/telegram-api ./cmd/telegram-api
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME) $(BUILD_DIR)/telegram-mcp $(BUILD_DIR)/telegram-api"

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
