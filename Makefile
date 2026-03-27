.PHONY: build test lint run-orchestrator docker-up docker-up-local docker-down

build:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

run-orchestrator:
	go run ./cmd/kimitsu start orchestrator

docker-up:
	docker compose -f deploy/docker-compose.yaml up --build

docker-up-local:
	docker compose -f deploy/docker-compose.local.yaml up --build

docker-down:
	docker compose -f deploy/docker-compose.yaml down
	docker compose -f deploy/docker-compose.local.yaml down
