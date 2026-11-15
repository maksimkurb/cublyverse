#!/usr/bin/env bash
#
# Consistent PostgreSQL backup of the Compose stack.
#
#   deploy/backup.sh                    # writes deploy/backups/cublyverse-<ts>.dump
#   BACKUP_DIR=/srv/backups deploy/backup.sh
#
# Call it from cron on the host, for example every night at 03:17:
#
#   17 3 * * * cd /srv/cublyverse && deploy/backup.sh >> /var/log/cublyverse-backup.log 2>&1
#
# pg_dump runs inside the database container, so no client tools are needed on
# the host. The custom format is restored with pg_restore (docs/DEPLOY.md).
# Old dumps are removed after BACKUP_RETENTION_DAYS days (default 14).

set -euo pipefail

cd "$(dirname "$0")"

if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

backup_dir="${BACKUP_DIR:-./backups}"
retention_days="${BACKUP_RETENTION_DAYS:-14}"
database="${POSTGRES_DB:-cublyverse}"
username="${POSTGRES_USER:-cublyverse}"

mkdir -p "$backup_dir"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="$backup_dir/cublyverse-$stamp.dump"
partial="$file.partial"

# -T: no TTY, so the dump goes to stdout. The dump is written to a .partial file
# first: a truncated backup must never look like a complete one.
if ! docker compose -f docker-compose.yml exec -T db \
  pg_dump --format=custom --no-owner --username "$username" --dbname "$database" > "$partial"; then
  rm -f "$partial"
  echo "backup failed: pg_dump returned an error" >&2
  exit 1
fi
mv "$partial" "$file"

# A dump smaller than a few kilobytes cannot contain a schema.
size="$(wc -c < "$file")"
if [ "$size" -lt 4096 ]; then
  echo "backup failed: $file is only $size bytes" >&2
  exit 1
fi

find "$backup_dir" -maxdepth 1 -name 'cublyverse-*.dump' -type f \
  -mtime "+$retention_days" -delete

echo "backup written: $file ($size bytes)"
