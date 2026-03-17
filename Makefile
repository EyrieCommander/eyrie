BINARY_NAME := eyrie
BUILD_DIR := bin
VERSION := 0.1.0
LDFLAGS := -ldflags "-X github.com/natalie/eyrie/internal/config.Version=$(VERSION)"

.PHONY: build dev dev-go dev-web clean test lint web install

build: web embed
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/eyrie

# Run both Go (air) and Vite dev servers. Ctrl-C stops both.
dev: dev-static
	@trap 'kill 0' EXIT; \
	cd web && npm run dev & \
	cd $(CURDIR) && $(HOME)/go/bin/air & \
	wait

# Run only the Go backend with auto-reload
dev-go: dev-static
	$(HOME)/go/bin/air

# Run only the Vite frontend dev server
dev-web:
	cd web && npm run dev

# Ensure static dir has a placeholder so //go:embed compiles in dev mode
dev-static:
	@mkdir -p internal/server/static
	@test -f internal/server/static/index.html || \
		echo '<!doctype html><html><body>Use Vite dev server</body></html>' > internal/server/static/index.html

NODE22_BIN := $(firstword $(wildcard $(HOME)/.nvm/versions/node/v22.*/bin))

web:
	@if [ -d web/node_modules ]; then \
		if [ -n "$(NODE22_BIN)" ]; then \
			cd web && PATH="$(NODE22_BIN):$$PATH" npm run build; \
		else \
			cd web && npm run build; \
		fi; \
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
