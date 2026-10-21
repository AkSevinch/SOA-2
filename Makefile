.PHONY: generate build run up down logs vet fmt

generate:
	go generate ./...

build: generate
	go build -o bin/server ./cmd/server

run: build
	./bin/server

up:
	docker compose up --build -d

down:
	docker compose down -v

logs:
	docker compose logs -f

vet:
	go vet ./...

fmt:
	gofmt -w .
