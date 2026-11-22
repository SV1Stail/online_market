ARG GO_IMAGE=golang:1.27.2-alpine3.24
FROM ${GO_IMAGE}

ARG PROTOC_GEN_GO_VERSION=v1.36.12
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
ARG PROTOC_GEN_GRPC_GATEWAY_VERSION=v2.32.0

RUN apk add --no-cache protobuf protobuf-dev \
    && GOBIN=/usr/local/bin go install \
        google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} \
    && GOBIN=/usr/local/bin go install \
        google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION} \
    && GOBIN=/usr/local/bin go install \
        github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@${PROTOC_GEN_GRPC_GATEWAY_VERSION} \
    && GOBIN=/usr/local/bin go install \
        github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@${PROTOC_GEN_GRPC_GATEWAY_VERSION} \
    && rm -rf /go/pkg/mod /root/.cache/go-build

WORKDIR /workspace/proto

ENTRYPOINT ["protoc"]

