#!/bin/sh

set -eu

if [ -z "${DATABASE_URL:-}" ]; then
    echo "DATABASE_URL is required" >&2
    exit 1
fi

exec migrate -path=/migrations -database="${DATABASE_URL}" up
