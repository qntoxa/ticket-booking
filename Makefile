.PHONY: run test race bench docker-up docker-down

run:
	go run ./cmd/booking/main.go

test:
	go test -v ./...

race:
	go test -race -v ./...

bench:
	go test -bench=. -benchmem ./internal/service/...

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v
