.PHONY: build run test migrate-up migrate-down docker-up docker-down dev lint

BINARY=lanops-tournament-manager
CMD=./cmd/server

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

dev:
	go run $(CMD)

test:
	go test ./... -v

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build

docker-down:
	docker compose down

docker-db:
	docker compose up -d postgres

migrate-up:
	go run $(CMD) migrate up

clean:
	rm -f $(BINARY)
