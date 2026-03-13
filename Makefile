BINARY_NAME := eyrie
BUILD_DIR := bin
VERSION := 0.1.0
LDFLAGS := -ldflags "-X github.com/natalie/eyrie/internal/config.Version=$(VERSION)"

.PHONY: build dev clean test lint web install

build: web embed
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/eyrie

dev:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/eyrie

web:
	@if [ -d web/node_modules ]; then \
		cd web && npm run build; \
	else \
		echo "Skipping web build (run 'cd web && npm install' first)"; \
		mkdir -p web/dist && echo '<!doctype html><html><body>Dashboard not built</body></html>' > web/dist/index.html; \
	fi

embed: web
	rm -rf internal/server/static
	mkdir -p internal/server/static
	cp -r web/dist/* internal/server/static/

clean:
	rm -rf $(BUILD_DIR) web/dist

test:
	go test ./...

lint:
	go vet ./...

install: build
	mkdir -p $(HOME)/.local/bin
	cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)
