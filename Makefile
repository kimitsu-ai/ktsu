.PHONY: build test lint run-orchestrator docker-up docker-up-local docker-down

build:
	go build -o ktsu ./cmd/ktsu

test:
	go test ./...

lint:
	go vet ./...

run-orchestrator:
	go run ./cmd/ktsu start orchestrator

COMPOSE_ENV := $(if $(wildcard .env),--env-file .env,)

docker-up:
	docker compose -f deploy/docker-compose.yaml $(COMPOSE_ENV) up --build

docker-up-local:
	docker compose -f deploy/docker-compose.local.yaml $(COMPOSE_ENV) up --build

docker-down:
	docker compose -f deploy/docker-compose.yaml $(COMPOSE_ENV) down
	docker compose -f deploy/docker-compose.local.yaml $(COMPOSE_ENV) down
