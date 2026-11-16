ARG GO_IMAGE=golang:1.27.2-alpine3.24
FROM ${GO_IMAGE}

ARG PROTOC_GEN_GO_VERSION=v1.36.12
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2

RUN apk add --no-cache protobuf protobuf-dev \
    && GOBIN=/usr/local/bin go install \
        google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} \
    && GOBIN=/usr/local/bin go install \
        google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION} \
    && rm -rf /go/pkg/mod /root/.cache/go-build

WORKDIR /workspace/proto

ENTRYPOINT ["protoc"]

