FROM golang:1.27.2-alpine3.24 AS builder

WORKDIR /src/backend

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/online-market-api ./cmd/api

FROM alpine:3.24

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder /out/online-market-api /usr/local/bin/online-market-api

USER app
EXPOSE 8080

ENTRYPOINT ["online-market-api"]

