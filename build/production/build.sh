#!/bin/sh

set -eu

usage() {
    echo "Usage: $0 <version>" >&2
    echo "Example: IMAGE_PREFIX=registry.example.com/team/online-market $0 1.0.0" >&2
}

version=${1:-}
case "${version}" in
    ""|[!A-Za-z0-9]*|*[!A-Za-z0-9._-]*)
        usage
        exit 2
        ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_dir}/../.." && pwd)

if ! command -v docker >/dev/null 2>&1; then
    echo "docker is not installed or not available in PATH" >&2
    exit 1
fi

image_prefix=${IMAGE_PREFIX:-online-market}
target_platform=${TARGET_PLATFORM:-linux/amd64}
api_image=${API_IMAGE:-${image_prefix}-api:${version}}
migrations_image=${MIGRATIONS_IMAGE:-${image_prefix}-migrations:${version}}
output_file=${OUTPUT_ENV_FILE:-${script_dir}/.images.env}

revision=$(git -C "${repository_root}" rev-parse --verify HEAD 2>/dev/null || echo unknown)
build_date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

docker build \
    --platform "${target_platform}" \
    --file "${script_dir}/backend.Dockerfile" \
    --build-arg "VERSION=${version}" \
    --build-arg "REVISION=${revision}" \
    --build-arg "BUILD_DATE=${build_date}" \
    --tag "${api_image}" \
    "${repository_root}"

docker build \
    --platform "${target_platform}" \
    --file "${script_dir}/migrations.Dockerfile" \
    --build-arg "VERSION=${version}" \
    --build-arg "REVISION=${revision}" \
    --build-arg "BUILD_DATE=${build_date}" \
    --tag "${migrations_image}" \
    "${repository_root}"

umask 077
{
    echo "API_IMAGE=${api_image}"
    echo "MIGRATIONS_IMAGE=${migrations_image}"
} > "${output_file}"

if [ "${PUSH:-0}" = "1" ]; then
    docker push "${api_image}"
    docker push "${migrations_image}"
fi

echo "Built ${api_image}"
echo "Built ${migrations_image}"
echo "Image manifest: ${output_file}"
