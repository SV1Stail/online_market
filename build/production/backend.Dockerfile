# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.27.2-alpine3.24
ARG RUNTIME_IMAGE=alpine:3.24

FROM ${GO_IMAGE} AS builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
COPY backend/vendor ./vendor
COPY backend/gen ./gen
COPY backend/internal ./internal
COPY backend/cmd ./cmd

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" \
    -o /out/online-market-api ./cmd/api \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" \
    -o /out/online-market-admin ./cmd/admin

FROM ${RUNTIME_IMAGE}

ARG VERSION
ARG REVISION
ARG BUILD_DATE

LABEL org.opencontainers.image.title="online-market-backend" \
      org.opencontainers.image.description="Online Market gRPC backend" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}"

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder --chown=app:app /out/online-market-api /usr/local/bin/online-market-api
COPY --from=builder --chown=app:app /out/online-market-admin /usr/local/bin/online-market-admin

USER app
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD nc -z 127.0.0.1 8080 || exit 1

ENTRYPOINT ["online-market-api"]
