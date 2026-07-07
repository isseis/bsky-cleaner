#!/bin/sh
# entrypoint.sh — container startup script for bsky-cleaner.
#
# 1. Run print-schedule to validate and retrieve the cron expression.
# 2. If print-schedule fails (non-zero exit), abort — fail-closed (AC-10).
# 3. Otherwise, write a crontab line that runs bsky-cleaner --apply on the
#    schedule, then exec supercronic so signals reach the cron process (AC-09).

if ! SCHEDULE=$(bsky-cleaner print-schedule --config "$BSKY_CONFIG_PATH"); then
    echo "print-schedule failed; aborting container startup" >&2
    exit 1
fi

printf '%s bsky-cleaner --apply --config "%s"\n' "$SCHEDULE" "$BSKY_CONFIG_PATH" > /tmp/crontab

exec supercronic /tmp/crontab
