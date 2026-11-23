SHELL := /bin/bash
COMPOSE_FILE := deploy/docker-compose.yml
ENV_FILE := $(shell test -f .env && echo .env || echo .env.example)

include $(ENV_FILE)
export

# Resolve Wails CLI binary: check PATH, then GOPATH/bin
GOPATH_BIN := $(shell go env GOPATH)/bin
WAILS_BIN := $(shell command -v wails 2>/dev/null || (test -x $(GOPATH_BIN)/wails && echo $(GOPATH_BIN)/wails))

# Detect installed webkit2gtk package on Linux (supports both 4.1 and legacy 4.0)
HAS_WEBKIT_41 := $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo "yes" || echo "no")
HAS_WEBKIT_40 := $(shell pkg-config --exists webkit2gtk-4.0 2>/dev/null && echo "yes" || echo "no")
HAS_GTK3 := $(shell pkg-config --exists gtk+-3.0 2>/dev/null && echo "yes" || echo "no")

ifeq ($(HAS_WEBKIT_41),yes)
  WAILS_TAGS := -tags webkit2_41
else
  WAILS_TAGS :=
endif

.PHONY: help up up-gpu down models dev dev-server build test smoke clean

help:
	@echo "Available targets:"
	@echo "  make up          - Start Ollama and LiteLLM services in the background (CPU default)"
	@echo "  make up-gpu      - Start Ollama with NVIDIA GPU acceleration profile"
	@echo "  make down        - Stop and remove Docker Compose containers"
	@echo "  make models      - Pull the configured Ollama model ($(OLLAMA_MODEL))"
	@echo "  make dev         - Run application in development mode (Wails desktop or standalone browser server)"
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
	@if [ -n "$(WAILS_BIN)" ] && [ "$(HAS_GTK3)" = "yes" ] && ([ "$(HAS_WEBKIT_41)" = "yes" ] || [ "$(HAS_WEBKIT_40)" = "yes" ]); then \
		echo "Starting Wails native desktop app in dev mode ($(WAILS_BIN))..."; \
		(cd app && $(WAILS_BIN) dev $(WAILS_TAGS)); \
	else \
		echo "================================================================================"; \
		if [ -z "$(WAILS_BIN)" ]; then \
			echo "Notice: Wails CLI ('wails') was not found in PATH or $(GOPATH_BIN)."; \
		fi; \
		if [ "$(HAS_WEBKIT_41)" != "yes" ] && [ "$(HAS_WEBKIT_40)" != "yes" ]; then \
			echo "Notice: Linux WebKit2GTK headers ('webkit2gtk-4.1' / 'webkit2gtk-4.0') not installed."; \
		fi; \
		echo "Starting GPU Assistant in standalone HTTP server mode (browser UI + Go backend)..."; \
		echo "URL: http://127.0.0.1:34115"; \
		echo "================================================================================"; \
		(cd app/frontend && (test -d dist || npm run build)) && \
		(cd app && HTTP_ADDR="127.0.0.1:34115" go run .); \
	fi

dev-server:
	@echo "Building frontend dist and starting standalone HTTP server on http://127.0.0.1:34115..."
	(cd app/frontend && (test -d dist || npm run build))
	(cd app && HTTP_ADDR="127.0.0.1:34115" go run .)

build:
	@echo "Building frontend..."
	(cd app/frontend && npm install && npm run build)
	@if [ -n "$(WAILS_BIN)" ] && [ "$(HAS_GTK3)" = "yes" ] && ([ "$(HAS_WEBKIT_41)" = "yes" ] || [ "$(HAS_WEBKIT_40)" = "yes" ]); then \
		echo "Building Wails desktop application ($(WAILS_BIN))..."; \
		(cd app && $(WAILS_BIN) build -clean -s $(WAILS_TAGS)); \
	else \
		echo "Building standalone desktop server binary (embedded frontend)..."; \
		mkdir -p app/build/bin; \
		(cd app && go build -o build/bin/gpu-assistant .); \
		echo "Binary built successfully at app/build/bin/gpu-assistant"; \
	fi

test:
	@echo "Running frontend unit tests and production bundle check..."
	(cd app/frontend && npm test && npm run build)
	@echo "Running backend unit and end-to-end tests..."
	(cd app && go test -v -race ./...)

smoke:
	@echo "Checking LiteLLM health endpoint..."
	@curl -sf http://127.0.0.1:$(LITELLM_PORT)/health/liveliness > /dev/null && echo "LiteLLM is healthy!" || (echo "LiteLLM unreachable" && exit 1)
	@echo "Checking LiteLLM model listing..."
	@curl -sf -H "Authorization: Bearer $(LITELLM_MASTER_KEY)" http://127.0.0.1:$(LITELLM_PORT)/v1/models | grep -q "gpu-assistant" && echo "Model 'gpu-assistant' is registered!" || (echo "Model not found" && exit 1)

clean:
	rm -rf app/build/bin app/frontend/dist
