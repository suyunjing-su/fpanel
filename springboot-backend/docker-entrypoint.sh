#!/bin/sh
set -eu

mkdir -p /app/data
chown -R flux:flux /app/data
exec su-exec flux "$@"
