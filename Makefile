# morrow-api — shortcuts for the Go service.
# DATABASE_URL is exported to every recipe; override it on the command line.

DATABASE_URL ?= postgres://secondbrain:secondbrain@127.0.0.1:5433/secondbrain
export DATABASE_URL

.PHONY: run build fmt vet test migrate-up migrate-down migrate-status db-reset db storage vapid tidy

run:
	go run ./cmd/migrate up && go run ./cmd/server

build:
	go build -trimpath -o bin/morrow-api ./cmd/server

fmt:
	gofmt -l -w .

vet:
	go vet ./...

test:
	go test ./... -count=1

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status


db-reset:
	go run ./cmd/migrate down && go run ./cmd/migrate up


db:
	docker compose up -d db

vapid:
	go run ./cmd/vapid

tidy:
	go mod tidy
