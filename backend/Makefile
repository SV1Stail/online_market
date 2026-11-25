GO ?= go
MIGRATE_IMAGE ?= migrate/migrate:v4.18.3
APP_IMAGE ?= online-market-api:local
MIGRATION_DIR := $(CURDIR)/migration

.PHONY: format test test-e2e build run admin-create migrate-up migrate-down docker-build

format:
	$(GO) fmt ./...

test:
	$(GO) test ./...

# Runs the in-process MVP scenario with mocked infrastructure.
test-e2e:
	$(GO) test ./tests/e2e/...

build:
	$(GO) build -o bin/api ./cmd/api

run:
	$(GO) run ./cmd/api

admin-create:
	$(GO) run ./cmd/admin

docker-build:
	docker build --file build/app/Dockerfile --tag $(APP_IMAGE) ..

migrate-up:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is required"; exit 1; }
	docker run --rm \
		--volume "$(MIGRATION_DIR):/migrations:ro" \
		$(MIGRATE_IMAGE) \
		-path=/migrations -database="$(DATABASE_URL)" up

migrate-down:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is required"; exit 1; }
	docker run --rm \
		--volume "$(MIGRATION_DIR):/migrations:ro" \
		$(MIGRATE_IMAGE) \
		-path=/migrations -database="$(DATABASE_URL)" down 1

