PROTO_SOURCE := ../backend/api/proto
PROTO_FILES := $(shell cd $(PROTO_SOURCE) && find online_market/v1 -type f -name '*.proto' -print | sort)
PROTO_IMAGE ?= online-market-contracts-generator:local
PROTO_GO_IMAGE ?= golang:1.27.2-alpine3.24
PROTOC_GEN_GO_VERSION ?= v1.36.12
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.2
PROTOC_GEN_GRPC_GATEWAY_VERSION ?= v2.32.0

.PHONY: generate generator-build source-check

source-check:
	@test -n "$(PROTO_FILES)" || { echo "No .proto files found in $(PROTO_SOURCE)/online_market/v1"; exit 1; }
	@test -f "$(PROTO_SOURCE)/http/online_market_v1.yaml" || { echo "HTTP mapping is missing"; exit 1; }

generator-build:
	docker build \
		--build-arg GO_IMAGE=$(PROTO_GO_IMAGE) \
		--build-arg PROTOC_GEN_GO_VERSION=$(PROTOC_GEN_GO_VERSION) \
		--build-arg PROTOC_GEN_GO_GRPC_VERSION=$(PROTOC_GEN_GO_GRPC_VERSION) \
		--build-arg PROTOC_GEN_GRPC_GATEWAY_VERSION=$(PROTOC_GEN_GRPC_GATEWAY_VERSION) \
		--tag $(PROTO_IMAGE) \
		../backend/build/proto

generate: source-check generator-build
	@mkdir -p gen
	docker run --rm \
		--user "$(shell id -u):$(shell id -g)" \
		--volume "$(abspath $(PROTO_SOURCE)):/workspace/proto:ro" \
		--volume "$(CURDIR)/gen:/workspace/out" \
		$(PROTO_IMAGE) \
		--proto_path=. \
		--proto_path=/usr/include \
		--go_out=/workspace/out \
		--go_opt=paths=source_relative \
		--go-grpc_out=/workspace/out \
		--go-grpc_opt=paths=source_relative,require_unimplemented_servers=true \
		--grpc-gateway_out=/workspace/out \
		--grpc-gateway_opt=paths=source_relative \
		--grpc-gateway_opt=grpc_api_configuration=http/online_market_v1.yaml \
		$(PROTO_FILES)
