# Aaru API (Go). Tests need Postgres: `docker compose up -d` here, or set POSTGRES_*.
.PHONY: generate api sqlc build test lint fmt run migrate

generate: api sqlc

api:
	./scripts/generate-api.sh

sqlc:
	go tool sqlc generate

build:
	go build -o bin/aaru ./cmd/aaru

test:
	go test ./...

lint:
	golangci-lint run ./...

fmt:
	go tool gofumpt -w .

run:
	go run ./cmd/aaru

migrate:
	go run ./cmd/aaru --db-migrate

check-fmt: ## Format validation — list files with wrong formatting
	@bad=$$(find . -name '*.go' -not -path './vendor/*' | xargs gofmt -l); \
	if [ -n "$$bad" ]; then \
		echo "The following files need gofmt:"; echo "$$bad"; exit 1; \
	fi
	@echo "gofmt: OK"

vet: ## Built-in static analysis
	go vet ./...

staticcheck: ## Advanced static analysis (requires staticcheck)
	staticcheck ./...

test-race: ## Unit tests with race detection (×10)
	go test -race -count=10 ./...

check-all: check-fmt vet staticcheck test-race ## Run all checks (fmt, vet, staticcheck, tests)