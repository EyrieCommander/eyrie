BINARY_NAME := eyrie
BUILD_DIR := bin
VERSION := 0.1.0
LDFLAGS := -ldflags "-X github.com/Audacity88/eyrie/internal/config.Version=$(VERSION)"
GOBIN := $(shell go env GOPATH)/bin

# Disable built-in Modula-2 rules that try to compile go.mod with m2c
%: %.mod
%.o: %.mod

.PHONY: build dev dev-go dev-web clean test lint web install uninstall ensure-air

build: web embed
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/eyrie

# Install air (Go live-reload) if missing
ensure-air:
	@command -v air >/dev/null 2>&1 || test -x $(GOBIN)/air || \
		(echo "Installing air..."; go install github.com/air-verse/air@latest)

# Run both Go (air) and Vite dev servers. Ctrl-C stops both.
dev: ensure-air dev-static
	@trap 'kill 0' EXIT; \
	$(GOBIN)/air & \
	echo "Waiting for backend to be ready..."; \
	while ! lsof -i :7200 >/dev/null 2>&1; do sleep 0.5; done; \
	echo "Backend ready, starting Vite..."; \
	cd web && npm run dev & \
	wait

# Run only the Go backend with auto-reload
dev-go: ensure-air dev-static
	$(GOBIN)/air

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
	@mkdir -p $(HOME)/.eyrie
	@test -f $(HOME)/.eyrie/registry.json || \
		(cp registry.json $(HOME)/.eyrie/registry.json && echo "Seeded ~/.eyrie/registry.json")
	@test -f $(HOME)/.eyrie/personas.json || \
		(cp personas.json $(HOME)/.eyrie/personas.json && echo "Seeded ~/.eyrie/personas.json")

uninstall:
	rm -f $(HOME)/.local/bin/$(BINARY_NAME)
