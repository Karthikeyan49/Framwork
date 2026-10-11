SHELL := /bin/sh
GO := ./scripts/go.sh
GO_PACKAGES := ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...
NPM := npm --cache $(CURDIR)/.local/cache/npm --prefix apps/web
COMPOSE := docker compose --env-file .env -f infra/compose/compose.yaml

.PHONY: setup env dev infra-up infra-down migrate migration-status api worker test test-unit test-integration check build fmt docs-verify
setup:
	./scripts/bootstrap.sh
env:
	node scripts/env.mjs
	node scripts/with-env.mjs node scripts/dev-identity.mjs
infra-up: env
	$(COMPOSE) up -d --wait --wait-timeout 90
infra-down:
	$(COMPOSE) down
migrate:
	node scripts/with-env.mjs $(GO) run ./cmd/migrate up
migration-status:
	node scripts/with-env.mjs $(GO) run ./cmd/migrate status
dev: setup infra-up migrate build
	node scripts/dev.mjs
api:
	node scripts/with-env.mjs $(GO) run ./cmd/api
worker:
	node scripts/with-env.mjs $(GO) run ./cmd/worker
test-unit:
	$(GO) test -race $(GO_PACKAGES)
	$(NPM) test
test-integration:
	node scripts/with-env.mjs $(GO) test -race -tags=integration -count=1 ./tests/integration
test: test-unit test-integration
check: docs-verify
	./scripts/check-format.sh
	$(GO) vet $(GO_PACKAGES)
	$(GO) mod verify
	$(NPM) run lint
	$(NPM) run typecheck
build:
	mkdir -p bin
	$(GO) build -o bin/api ./cmd/api
	$(GO) build -o bin/worker ./cmd/worker
	$(GO) build -o bin/migrate ./cmd/migrate
	$(GO) build -o bin/iam ./cmd/iam
	$(NPM) run build
fmt:
	find cmd internal db contracts tests -name '*.go' -type f -exec "$$($(GO) env GOROOT)/bin/gofmt" -w {} +
docs-verify:
	node scripts/verify-docs.mjs

.PHONY: test-e2e browser-install
browser-install:
	node scripts/browser-install.mjs
test-e2e:
	PLAYWRIGHT_BROWSERS_PATH=$(CURDIR)/.local/cache/ms-playwright $(NPM) run test:e2e
