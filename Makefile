SHELL := /bin/bash
COMPOSE_FILE := deploy/docker-compose.yml
ENV_FILE := $(shell test -f .env && echo .env || echo .env.example)

include $(ENV_FILE)
export

.PHONY: help up up-gpu down models dev build test smoke clean

help:
	@echo "Available targets:"
	@echo "  make up        - Start Ollama and LiteLLM services in the background (CPU default)"
	@echo "  make up-gpu    - Start Ollama with NVIDIA GPU acceleration profile"
	@echo "  make down      - Stop and remove Docker Compose containers"
	@echo "  make models    - Pull the configured Ollama model ($(OLLAMA_MODEL))"
	@echo "  make dev       - Run Wails in development mode (app/)"
	@echo "  make build     - Build production desktop binary and frontend"
	@echo "  make test      - Run Go backend and frontend unit tests"
	@echo "  make smoke     - Run smoke test verifying LiteLLM proxy and Ollama health"
	@echo "  make clean     - Remove build artifacts and temporary files"

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
	cd app && wails dev

build:
	@echo "Building frontend and Wails desktop application..."
	cd app/frontend && npm install && npm run build
	cd app && wails build -clean -s

test:
	@echo "Running frontend unit tests and production bundle check..."
	cd app/frontend && npm test && npm run build
	@echo "Running backend unit and end-to-end tests..."
	cd app && go test -v -race ./...

smoke:
	@echo "Checking LiteLLM health endpoint..."
	@curl -sf http://127.0.0.1:$(LITELLM_PORT)/health/liveliness > /dev/null && echo "LiteLLM is healthy!" || (echo "LiteLLM unreachable" && exit 1)
	@echo "Checking LiteLLM model listing..."
	@curl -sf -H "Authorization: Bearer $(LITELLM_MASTER_KEY)" http://127.0.0.1:$(LITELLM_PORT)/v1/models | grep -q "gpu-assistant" && echo "Model 'gpu-assistant' is registered!" || (echo "Model not found" && exit 1)

clean:
	rm -rf app/build/bin app/frontend/dist
