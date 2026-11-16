# syntax=docker/dockerfile:1.7

ARG MIGRATE_IMAGE=migrate/migrate:v4.18.3
FROM ${MIGRATE_IMAGE}

ARG VERSION
ARG REVISION
ARG BUILD_DATE

LABEL org.opencontainers.image.title="online-market-migrations" \
      org.opencontainers.image.description="Online Market database migrations" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}"

COPY --chmod=0444 backend/migration/ /migrations/
COPY --chmod=0555 build/production/run-migrations.sh /usr/local/bin/run-migrations

ENTRYPOINT ["/usr/local/bin/run-migrations"]
