#!/bin/sh
set -eu

# The service must never start against an unprepared production database.
# `up` is idempotent, so this also runs safely on every Render redeploy.
/app/migrate -path /app/migrations -database "$DATABASE_URL" up
exec /app/millena-api
