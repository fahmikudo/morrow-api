#!/usr/bin/env bash
#
# Roll the API stack forward to a new image tag.
#
#   deploy.sh MORROW_API_IMAGE_TAG 1.2.0 api
#
# The tag is written into /opt/morrow-api/.env rather than passed to `up`, so the
# file always states which version is running and a rollback is one edit plus
# `docker compose up -d`. CI copies this script to /opt/morrow-api/deploy.sh and
# runs it over SSH on every release tag. See DEPLOYMENT.md.

set -euo pipefail

readonly STACK_DIR="${STACK_DIR:-/opt/morrow-api}"

if [ "$#" -lt 3 ]; then
  echo "usage: $0 <ENV_VAR> <TAG> <service> [service...]" >&2
  exit 64
fi

var="$1"
tag="$2"
shift 2
services=("$@")

cd "${STACK_DIR}"

if [ ! -f .env ]; then
  echo "${STACK_DIR}/.env is missing; the stack has never been provisioned" >&2
  exit 1
fi

# Pull first, with the tag given for this command only: a tag that does not
# exist in GHCR fails here, before .env names a version the server cannot run.
echo "==> pulling ${var}=${tag}"
env "${var}=${tag}" docker compose pull "${services[@]}"

# Replace the line if it exists, append it if it does not. Everything else in
# the file — secrets and settings — is left untouched. Through a temporary file
# rather than `sed -i`, whose flag differs between GNU and BSD sed; `cat >`
# keeps the file's owner and mode 600.
if grep -q "^${var}=" .env; then
  tmp="$(mktemp)"
  sed "s|^${var}=.*|${var}=${tag}|" .env > "${tmp}"
  cat "${tmp}" > .env
  rm -f "${tmp}"
else
  printf '%s=%s\n' "${var}" "${tag}" >> .env
fi

echo "==> ${var}=${tag}"

# --wait returns once every service is running and healthy; a service that
# never gets healthy fails the deploy, and the workflow goes red.
if ! docker compose up -d --wait "${services[@]}"; then
  docker compose logs --tail=100 "${services[@]}" || true
  echo "deploy failed. To roll back: put the previous tag in ${STACK_DIR}/.env (${var}=...)" >&2
  echo "and run: cd ${STACK_DIR} && docker compose up -d" >&2
  exit 1
fi

# Only layers no tag points at; a rollback pulls older tags from GHCR again.
docker image prune -f

docker compose ps
