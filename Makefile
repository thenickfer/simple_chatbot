SHELL := /bin/bash
COMPOSE_FILE := deploy/docker-compose.yml
ENV_FILE := $(shell test -f .env && echo .env || echo .env.example)

include $(ENV_FILE)
export

# Detect Linux GTK & WebKit toolchains for Wails v3
HAS_GTK4_WEBKIT6 := $(shell pkg-config --exists gtk4 webkitgtk-6.0 2>/dev/null && echo "yes" || echo "no")
HAS_GTK3_WEBKIT4 := $(shell pkg-config --exists gtk+-3.0 webkit2gtk-4.1 2>/dev/null && echo "yes" || echo "no")

# Wails v3 uses GTK4 + WebKitGTK 6.0 by default. On older distros, -tags gtk3 enables GTK3 + WebKit2GTK 4.1
ifeq ($(HAS_GTK4_WEBKIT6),yes)
  WAILS_BUILD_TAGS :=
else ifeq ($(HAS_GTK3_WEBKIT4),yes)
  WAILS_BUILD_TAGS := -tags gtk3
else
  WAILS_BUILD_TAGS := -tags purego
endif

.PHONY: help up up-gpu down models dev dev-server build test smoke clean

help:
	@echo "Available targets:"
	@echo "  make up          - Start Ollama and LiteLLM services in the background (CPU default)"
	@echo "  make up-gpu      - Start Ollama with NVIDIA GPU acceleration profile"
	@echo "  make down        - Stop and remove Docker Compose containers"
	@echo "  make models      - Pull the configured Ollama model ($(OLLAMA_MODEL))"
	@echo "  make dev         - Run application in development mode (Wails v3 native desktop or standalone server)"
	@echo "  make dev-server  - Run standalone HTTP + embedded React UI server directly on :34115"
	@echo "  make build       - Build production binary and frontend"
	@echo "  make test        - Run Go backend and frontend unit tests"
	@echo "  make smoke       - Run smoke test verifying LiteLLM proxy and Ollama health"
	@echo "  make clean       - Remove build artifacts and temporary files"

up:
	docker compose -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d

up-gpu:
	docker compose -f $(COMPOSE_FILE) --profile gpu --env-file $(ENV_FILE) up -d

down:
	docker compose -f $(COMPOSE_FILE) down

models:
	@echo "Pulling model $(OLLAMA_MODEL) into Ollama container..."
	docker compose -f $(COMPOSE_FILE) exec ollama ollama pull $(OLLAMA_MODEL)

dev:
	@echo "Ensuring frontend is built..."
	@(cd app/frontend && (test -d dist || npm run build))
	@if [ -n "$$DISPLAY" ] || [ -n "$$WAYLAND_DISPLAY" ]; then \
		if [ "$(HAS_GTK4_WEBKIT6)" = "yes" ]; then \
			echo "Starting Wails v3 native desktop application (GTK4 + WebKitGTK 6.0)..."; \
			(cd app && go run .); \
		elif [ "$(HAS_GTK3_WEBKIT4)" = "yes" ]; then \
			echo "Starting Wails v3 native desktop application (GTK3 fallback)..."; \
			(cd app && go run -tags gtk3 .); \
		else \
			echo "No GTK4 or GTK3 WebKit headers found. Falling back to standalone browser server..."; \
			echo "URL: http://127.0.0.1:34115"; \
			(cd app && HTTP_ADDR="127.0.0.1:34115" go run .); \
		fi \
	else \
		echo "No GUI display detected ($$DISPLAY/$$WAYLAND_DISPLAY unset). Starting standalone HTTP server..."; \
		echo "URL: http://127.0.0.1:34115"; \
		(cd app && HTTP_ADDR="127.0.0.1:34115" go run .); \
	fi

dev-server:
	@echo "Building frontend dist and starting standalone HTTP server on http://127.0.0.1:34115..."
	@(cd app/frontend && (test -d dist || npm run build))
	@(cd app && HTTP_ADDR="127.0.0.1:34115" go run .)

build:
	@echo "Building frontend..."
	@(cd app/frontend && (test -d node_modules || npm install) && npm run build)
	@echo "Building Wails v3 desktop binary ($(WAILS_BUILD_TAGS))..."
	@mkdir -p app/build/bin
	@(cd app && go build $(WAILS_BUILD_TAGS) -o build/bin/gpu-assistant .)
	@echo "Binary built successfully at app/build/bin/gpu-assistant"

test:
	@echo "Running frontend unit tests and production bundle check..."
	@(cd app/frontend && npm test && npm run build)
	@echo "Running backend unit and end-to-end tests..."
	@(cd app && go test -v -race ./...)

smoke:
	@echo "Checking LiteLLM health endpoint..."
	@curl -sf http://127.0.0.1:$(LITELLM_PORT)/health/liveliness > /dev/null && echo "LiteLLM is healthy!" || (echo "LiteLLM unreachable" && exit 1)
	@echo "Checking LiteLLM model listing..."
	@curl -sf -H "Authorization: Bearer $(LITELLM_MASTER_KEY)" http://127.0.0.1:$(LITELLM_PORT)/v1/models | grep -q "gpu-assistant" && echo "Model 'gpu-assistant' is registered!" || (echo "Model not found" && exit 1)

clean:
	rm -rf app/build/bin app/frontend/dist
