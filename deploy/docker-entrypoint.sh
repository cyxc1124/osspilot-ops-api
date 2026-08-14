#!/bin/sh
set -eu
cmd=${1:-api}
if [ "$#" -gt 0 ]; then
  shift
fi
case "$cmd" in
  api) exec /app/api "$@" ;;
  migrate) exec /app/migrate "$@" ;;
  worker) exec /app/worker "$@" ;;
  reset-password) exec /app/reset-password "$@" ;;
  *) exec "$cmd" "$@" ;;
esac
