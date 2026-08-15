# Program metadata
BINARY_NAME=zim-updater
BUILD_DIR=build
VERSION?=1.0.1
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all clean build deps test build-all build-linux build-darwin build-windows build-arm

all: build

## deps: Download dependencies
deps:
	go mod tidy
	go mod download

## build: Build binary for local architecture
build: deps
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "✓ Built $(BUILD_DIR)/$(BINARY_NAME)"

## build-all: Cross-compile for Linux, macOS, and Windows (x86_64 and ARM64)
build-all: build-linux build-darwin build-windows build-arm

build-linux: deps
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	@echo "✓ Built Linux amd64"

build-darwin: deps
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .
	@echo "✓ Built macOS (amd64 & arm64)"

build-windows: deps
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .
	@echo "✓ Built Windows amd64"

build-arm: deps
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	GOOS=linux GOARCH=arm7 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 .
	@echo "✓ Built Linux ARM (arm64 & armv7)"

## clean: Remove build directory
clean:
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned build artifacts"

## test: Run tests
test:
	go test -v ./...