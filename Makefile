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
	node scripts/m8-env.mjs
	node scripts/with-env.mjs node scripts/dev-identity.mjs
infra-up: env
	$(COMPOSE) up -d --wait --wait-timeout 90
	node scripts/with-env.mjs node scripts/signin-roles.mjs
.PHONY: reference-up
reference-up: env
	@test "$$(docker info --format '{{.MemTotal}}')" -ge 8589934592 || { echo 'Reference profile requires at least 8GiB Docker memory'; exit 1; }
	$(COMPOSE) -f infra/compose/reference-performance.yaml up -d --wait --wait-timeout 90 postgres
infra-down:
	$(COMPOSE) down
migrate: roles-init
	node scripts/with-env.mjs $(GO) run ./cmd/migrate up
migration-status:
	node scripts/with-env.mjs $(GO) run ./cmd/migrate status
dev: setup infra-up migrate build
	node scripts/dev.mjs
api:
	node scripts/with-env.mjs $(GO) run ./cmd/api
worker:
	node scripts/with-env.mjs $(GO) run ./cmd/worker
dispatcher:
	node scripts/with-env.mjs $(GO) run ./cmd/dispatcher
test-unit:
	$(GO) test -race $(GO_PACKAGES)
	node --test scripts/*.test.mjs
	$(NPM) test
# Aggregate suite deadline; individual runtime/query/lock budgets are unchanged.
test-integration:
	node scripts/with-env.mjs $(GO) test -race -tags=integration -timeout=90m -count=1 ./tests/integration
.PHONY: test-performance test-write-performance test-scalar-performance test-choice-performance test-native-performance test-search-performance
test-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$$' -v ./tests/integration
test-write-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -count=1 -run '^TestM9RIndependentWritePerformanceFloor$$' -v ./tests/integration
test-scalar-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RScalarIndexPerformanceFloor$$' -v ./tests/integration
test-choice-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RChoiceIndexPerformanceFloor$$' -v ./tests/integration
test-native-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RNativeIndexPerformanceFloor$$' -v ./tests/integration
test-search-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RSearchPerformanceFloor$$' -v ./tests/integration
.PHONY: test-unique-performance
test-unique-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RUnique(Constraint|Write)PerformanceFloor$$' -v ./tests/integration
.PHONY: test-aggregate-performance
test-aggregate-performance:
	node scripts/with-env.mjs $(GO) test -tags=integration,performance -timeout=20m -count=1 -failfast -run '^TestM9RSQLAggregate(Mapped)?PerformanceFloor$$' -v ./tests/integration
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
	$(GO) build -o bin/dispatcher ./cmd/dispatcher
	$(GO) build -o bin/event-replay ./cmd/event-replay
	$(GO) build -o bin/migrate ./cmd/migrate
	$(GO) build -o bin/iam ./cmd/iam
	$(GO) build -o bin/worker-admin ./cmd/worker-admin
	$(GO) build -o bin/release-check ./cmd/release-check
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

.PHONY: roles-init
roles-init:
	node scripts/m6r-env.mjs
	node scripts/with-env.mjs node scripts/m6r-roles.mjs

.PHONY: release-check
release-check:
	node scripts/with-env.mjs $(GO) run ./cmd/release-check

.PHONY: test-rules-fuzz
RULE_FUZZ_TIME ?= 10s
test-rules-fuzz:
	GOMAXPROCS=2 $(GO) test -run '^$$' -fuzz '^FuzzRuleCompile$$' -fuzztime=$(RULE_FUZZ_TIME) -parallel=2 ./internal/runtime/rules
	GOMAXPROCS=2 $(GO) test -run '^$$' -fuzz '^FuzzRuleRestoreAndInputs$$' -fuzztime=$(RULE_FUZZ_TIME) -parallel=2 ./internal/runtime/rules
	GOMAXPROCS=2 $(GO) test -run '^$$' -fuzz '^FuzzFixedDecimalMoney$$' -fuzztime=$(RULE_FUZZ_TIME) -parallel=2 ./internal/runtime/rules

.PHONY: test-events-fuzz
test-events-fuzz:
	GOMAXPROCS=2 $(GO) test -run '^$$' -fuzz '^FuzzPointer$$' -fuzztime=10s -parallel=2 ./internal/events

.PHONY: test-event-outage
# Real ten-minute consumer absence and fixture-owned Redis sender rejection.
test-event-outage:
	node scripts/with-env.mjs $(GO) test -race -tags=integration,eventoutage -timeout=15m -count=1 -run '^TestR7bRealTenMinuteEventOutage$$' -v ./tests/integration

.PHONY: test-workflows-fuzz
test-workflows-fuzz:
	GOMAXPROCS=2 $(GO) test -run '^$$' -fuzz '^FuzzWorkflowRestore$$' -fuzztime=10s -parallel=2 ./internal/runtime/workflow

.PHONY: test-signin-e2e
test-signin-e2e:
	PLAYWRIGHT_BROWSERS_PATH=$(CURDIR)/.local/cache/ms-playwright $(NPM) run test:signin-e2e
