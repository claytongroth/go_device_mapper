#!/usr/bin/env bash
# Dev helper for the docker-compose stack (devicesim + apiserver).
#
#   scripts/dev.sh up        rebuild images, run in the foreground with logs
#   scripts/dev.sh down      stop and remove containers + network
#   scripts/dev.sh restart   down, then up
#   scripts/dev.sh logs      tail logs of an already-running stack
#
# `up` always rebuilds (--build), so code changes are picked up without a
# separate build step, and it cleans up after itself on exit (Ctrl-C or
# otherwise) via the trap below — you should never need to manually run
# `down` just because you stopped watching the logs.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

usage() {
  echo "Usage: $0 [up|down|restart|logs]"
  exit 1
}

[[ $# -eq 1 ]] || usage

case "$1" in
  up)
    trap 'docker compose down --remove-orphans' EXIT
    docker compose up --build
    ;;
  down)
    docker compose down --remove-orphans
    ;;
  restart)
    docker compose down --remove-orphans
    trap 'docker compose down --remove-orphans' EXIT
    docker compose up --build
    ;;
  logs)
    docker compose logs -f
    ;;
  *)
    usage
    ;;
esac
