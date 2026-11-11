# airval build entry points. `make check` must pass before every commit.
SHELL := /bin/bash
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
export CGO_ENABLED := 0
AIRVAL_TEST_PG_URL ?= postgres://postgres:airval@localhost:55432/airval?sslmode=disable
GOLANGCI_VERSION ?= v2.6.2

.PHONY: all build build-ee web gen gen-api test test-pg pg-up lint lint-full check dev bench clean

all: build

## build: build the UI and the single static binary (bin/airval)
build: web
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/airval ./cmd/airval

## build-ee: binary with enterprise features
build-ee: web
	$(GO) build -trimpath -tags ee -ldflags '$(LDFLAGS)' -o bin/airval-ee ./cmd/airval

## web: build web/dist (embedded into the binary)
web:
	@if [ -f web/package.json ]; then \
		[ -d web/node_modules ] || npm --prefix web ci; \
		npm --prefix web run build; \
		touch web/dist/.gitkeep; \
	else echo "web/ not present; skipping UI build"; fi

## gen: regenerate the Go server (oapi-codegen) and the TS client from api/openapi.yaml
gen: gen-api
gen-api:
	cd internal/api/gen && $(GO) tool oapi-codegen -config cfg.yaml ../../../api/openapi.yaml
	@if [ -d web/node_modules ]; then npm --prefix web run gen:api; fi

## test: Go unit + SQLite contract tests
test:
	$(GO) test ./...

## pg-up: start the Postgres container used by test-pg
pg-up:
	@docker start airval-pg >/dev/null 2>&1 || docker run -d --name airval-pg -e POSTGRES_PASSWORD=airval \
		-e POSTGRES_DB=airval -p 55432:5432 postgres:17-alpine >/dev/null
	@for i in $$(seq 1 30); do docker exec airval-pg pg_isready -U postgres >/dev/null 2>&1 && exit 0; sleep 1; done; \
		echo "postgres did not become ready"; exit 1

## test-pg: store contract tests against Postgres too
test-pg: pg-up
	AIRVAL_TEST_PG_URL='$(AIRVAL_TEST_PG_URL)' $(GO) test ./internal/store/... ./internal/api/...

## lint: go vet (core and ee) + gofmt
lint:
	$(GO) vet ./...
	$(GO) vet -tags ee ./...
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go' | grep -v '\.gen\.go$$')); \
		if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## lint-full: golangci-lint (downloaded on demand via go run)
lint-full: lint
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

## check: what CI runs: Go lint + tests (Postgres too when docker is available) + web check
check: lint
	@if docker info >/dev/null 2>&1; then $(MAKE) --no-print-directory test-pg; fi
	$(GO) test ./...
	@if [ -f web/package.json ]; then npm --prefix web run check; fi

## dev: run the all-in-one server (SQLite + embedded NATS in ./airval-data)
dev:
	$(GO) run ./cmd/airval dev

## bench: execution-engine benchmarks (plan §3.4); placeholder until the engine lands
bench:
	$(GO) test -run '^$$' -bench . -benchmem ./internal/...

clean:
	rm -rf bin airval-data
