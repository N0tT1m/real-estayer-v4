#!/usr/bin/env bash
# backup.sh — dump MongoDB and optionally push the archive off-box.
#
# Usage:
#   MONGODB_URI=mongodb://... BACKUP_DIR=/var/backups ./scripts/backup.sh
#
# When BACKUP_S3_BUCKET is set the resulting .tar.gz is uploaded via awscli
# and then deleted locally. Intended for cron — logs to stderr, exits
# non-zero on failure, leaves a datestamped file otherwise.
#
# This script is deliberately dependency-light: bash + mongodump + tar +
# optional awscli. No cron wrapper; your scheduler adds that.

set -euo pipefail

MONGODB_URI="${MONGODB_URI:-mongodb://localhost:27017/real_estayer}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"

mkdir -p "$BACKUP_DIR"

timestamp="$(date -u +'%Y%m%dT%H%M%SZ')"
workdir="$(mktemp -d -t real-estayer-backup-XXXXXX)"
trap 'rm -rf "$workdir"' EXIT

echo "[$(date -u +%FT%TZ)] dumping $MONGODB_URI" >&2
mongodump --uri="$MONGODB_URI" --quiet --out="$workdir/dump"

archive="$BACKUP_DIR/real-estayer-$timestamp.tar.gz"
tar -C "$workdir" -czf "$archive" dump
echo "[$(date -u +%FT%TZ)] wrote $archive ($(du -h "$archive" | cut -f1))" >&2

if [ -n "${BACKUP_S3_BUCKET:-}" ]; then
  if ! command -v aws >/dev/null 2>&1; then
    echo "awscli not installed; keeping local copy" >&2
  else
    aws s3 cp "$archive" "s3://${BACKUP_S3_BUCKET}/real-estayer/$(basename "$archive")"
    rm -f "$archive"
    echo "[$(date -u +%FT%TZ)] uploaded to s3://${BACKUP_S3_BUCKET}/real-estayer/" >&2
  fi
fi

# Prune old local archives.
if [ -d "$BACKUP_DIR" ]; then
  find "$BACKUP_DIR" -type f -name 'real-estayer-*.tar.gz' -mtime +"$RETENTION_DAYS" -delete
fi
