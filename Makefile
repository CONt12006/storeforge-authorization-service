APP=authorization-service

.PHONY: run build test fmt docker-up docker-down

run:
	go run ./cmd/authorization-service

build:
	go build -o bin/$(APP) ./cmd/authorization-service

test:
	go test ./...

fmt:
	gofmt -w cmd internal

docker-up:
	docker compose up --build

docker-down:
	docker compose down
