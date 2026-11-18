#!/usr/bin/env bash
# QuickBite on Redis (Go Edition) -- run with Go. Needs Go 1.25+ and a running Redis (docker compose up -d).
#
#   ./run.sh                    start the API server on http://localhost:8080
#   ./run.sh check [A|B|C|D|E]  run the automatic checks (default: all tracks)
#   ./run.sh worker w1          start a kitchen worker named w1   (add --crash to simulate a crash)
set -e
cd "$(dirname "$0")"

case "${1:-app}" in
  check)  shift; go run . check "${@:-all}" ;;
  worker) shift; go run . worker "$@" ;;
  app)    go run . app ;;
  *)      echo "usage: ./run.sh [check [A|B|C|D|E] | worker <name> [--crash]]"; exit 1 ;;
esac
