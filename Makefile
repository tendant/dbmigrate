# Variables
BINARY_NAME=migrate
CMD_PATH=cmd/migrate
BUILD_DIR=bin
VERSION ?= $(shell git describe --tags --always --dirty)

# Default target
all: build

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_PATH)

clean:
	rm -rf $(BUILD_DIR)

run:
	./$(BUILD_DIR)/$(BINARY_NAME) run --config-path=samples/bootstrap.yaml

build-static:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_PATH)

# Updated buildx-based Docker image build
docker-build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		--tag wang/dbmigrate:$(VERSION) \
		--tag wang/dbmigrate:latest \
		.

