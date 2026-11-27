PROTO_ROOT := backend/api/proto
PROTO_GO_OUT := backend/gen
PROTO_OPENAPI_OUT := backend/gen/openapi
PROTO_PACKAGE := online_market/v1
PROTO_FILES := $(shell cd $(PROTO_ROOT) && find $(PROTO_PACKAGE) -type f -name '*.proto' -print | sort)
PROTO_HTTP_CONFIG := http/online_market_v1.yaml
PROTO_OPENAPI_CONFIG := http/online_market_v1.openapi.yaml
PROTO_DOCKER_CONTEXT := backend/build/proto
PROTO_DOCKER_IMAGE ?= online-market-proto-generator:local
PROTO_GO_IMAGE ?= golang:1.27.2-alpine3.24
PROTOC_GEN_GO_VERSION ?= latest
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.2
PROTOC_GEN_GRPC_GATEWAY_VERSION ?= latest
LOCAL_ENV ?= build/.env
LOCAL_COMPOSE := docker compose --env-file $(LOCAL_ENV) -f build/docker-compose.yml
PRODUCTION_VERSION ?=

.PHONY: proto-generate-docker proto-docker-build proto-tools-check docker-tools-check local-env-check backend-local-up backend-local-down backend-local-logs backend-local-ps backend-production-build backend-run backend-test backend-test-e2e backend-build

# Generates protobuf, gRPC, HTTP gateway and OpenAPI artifacts.
proto-generate: proto-tools-check
	@mkdir -p $(PROTO_GO_OUT)
	@mkdir -p $(PROTO_OPENAPI_OUT)
	@cd $(PROTO_ROOT) && protoc \
		--proto_path=. \
		--go_out=$(abspath $(PROTO_GO_OUT)) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(abspath $(PROTO_GO_OUT)) \
		--go-grpc_opt=paths=source_relative,require_unimplemented_servers=true \
		--grpc-gateway_out=$(abspath $(PROTO_GO_OUT)) \
		--grpc-gateway_opt=paths=source_relative \
		--grpc-gateway_opt=grpc_api_configuration=$(PROTO_HTTP_CONFIG) \
		--openapiv2_out=$(abspath $(PROTO_OPENAPI_OUT)) \
		--openapiv2_opt=grpc_api_configuration=$(PROTO_HTTP_CONFIG) \
		--openapiv2_opt=openapi_configuration=$(PROTO_OPENAPI_CONFIG) \
		--openapiv2_opt=allow_merge=true \
		--openapiv2_opt=merge_file_name=online_market \
		--openapiv2_opt=output_format=json \
		$(PROTO_FILES)

# Builds an isolated generator image with pinned protobuf and gateway plugins.
proto-docker-build: docker-tools-check
	@docker build \
		--build-arg GO_IMAGE=$(PROTO_GO_IMAGE) \
		--build-arg PROTOC_GEN_GO_VERSION=$(PROTOC_GEN_GO_VERSION) \
		--build-arg PROTOC_GEN_GO_GRPC_VERSION=$(PROTOC_GEN_GO_GRPC_VERSION) \
		--build-arg PROTOC_GEN_GRPC_GATEWAY_VERSION=$(PROTOC_GEN_GRPC_GATEWAY_VERSION) \
		--tag $(PROTO_DOCKER_IMAGE) \
		$(PROTO_DOCKER_CONTEXT)

# Generates code inside Docker and writes it to backend/gen through a bind mount.
proto-generate-docker: proto-docker-build
	@mkdir -p $(PROTO_GO_OUT)
	@mkdir -p $(PROTO_OPENAPI_OUT)
	@docker run --rm \
		--user "$(shell id -u):$(shell id -g)" \
		--volume "$(CURDIR)/$(PROTO_ROOT):/workspace/proto:ro" \
		--volume "$(CURDIR)/$(PROTO_GO_OUT):/workspace/out" \
		--volume "$(CURDIR)/$(PROTO_OPENAPI_OUT):/workspace/openapi" \
		$(PROTO_DOCKER_IMAGE) \
		--proto_path=. \
		--proto_path=/usr/include \
		--go_out=/workspace/out \
		--go_opt=paths=source_relative \
		--go-grpc_out=/workspace/out \
		--go-grpc_opt=paths=source_relative,require_unimplemented_servers=true \
		--grpc-gateway_out=/workspace/out \
		--grpc-gateway_opt=paths=source_relative \
		--grpc-gateway_opt=grpc_api_configuration=$(PROTO_HTTP_CONFIG) \
		--openapiv2_out=/workspace/openapi \
		--openapiv2_opt=grpc_api_configuration=$(PROTO_HTTP_CONFIG) \
		--openapiv2_opt=openapi_configuration=$(PROTO_OPENAPI_CONFIG) \
		--openapiv2_opt=allow_merge=true \
		--openapiv2_opt=merge_file_name=online_market \
		--openapiv2_opt=output_format=json \
		$(PROTO_FILES)

# Fails early with an actionable error and never installs tools implicitly.
proto-tools-check:
	@command -v protoc >/dev/null 2>&1 || { echo "protoc is not installed or not available in PATH"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "protoc-gen-go is not installed or not available in PATH"; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "protoc-gen-go-grpc is not installed or not available in PATH"; exit 1; }
	@command -v protoc-gen-grpc-gateway >/dev/null 2>&1 || { echo "protoc-gen-grpc-gateway is not installed or not available in PATH"; exit 1; }
	@command -v protoc-gen-openapiv2 >/dev/null 2>&1 || { echo "protoc-gen-openapiv2 is not installed or not available in PATH"; exit 1; }

docker-tools-check:
	@command -v docker >/dev/null 2>&1 || { echo "docker is not installed or not available in PATH"; exit 1; }

local-env-check:
	@test -f "$(LOCAL_ENV)" || { echo "$(LOCAL_ENV) is missing; copy build/.env.example and fill required values"; exit 1; }

backend-local-up: docker-tools-check local-env-check
	$(LOCAL_COMPOSE) up --build --detach
	@echo "gRPC API: localhost:8080"
	@echo "gRPC UI:  http://127.0.0.1:8081 (or GRPCUI_PORT from $(LOCAL_ENV))"

backend-local-down: docker-tools-check local-env-check
	$(LOCAL_COMPOSE) down --volumes

backend-local-logs: docker-tools-check local-env-check
	$(LOCAL_COMPOSE) logs --follow api grpc-ui

backend-local-ps: docker-tools-check local-env-check
	$(LOCAL_COMPOSE) ps

backend-production-build: docker-tools-check
	@test -n "$(PRODUCTION_VERSION)" || { echo "PRODUCTION_VERSION is required"; exit 1; }
	./build/production/build.sh "$(PRODUCTION_VERSION)"

backend-run:
	$(MAKE) -C backend run

backend-test:
	$(MAKE) -C backend test

backend-test-e2e:
	$(MAKE) -C backend test-e2e

backend-build:
	$(MAKE) -C backend build



