#!/bin/sh
set -eu
cd "$HOME/anubis-4090-bench"
case "${1:-}" in ''|*[!0-9]*) echo 'Expected bounded telemetry duration' >&2; exit 1;; esac
test "$1" -le 3700
end=$(($(date +%s) + $1))
while test "$(date +%s)" -lt "$end"; do
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    samples=''
    first=true
    for service in nginx anubis backend; do
        id=$(docker compose ps -q "$service")
        test -n "$id" || { echo "Missing benchmark service: $service" >&2; exit 1; }
        state=$(docker inspect --format '{{.State.Status}}' "$id")
        test "$state" = running || { echo "Service $service is $state" >&2; exit 1; }
        if test "$service" = anubis; then
            health=$(docker inspect --format '{{.State.Health.Status}}' "$id")
            test "$health" = healthy || { echo "Anubis health: $health" >&2; exit 1; }
        fi
        stats=$(docker stats --no-stream --format '{{json .}}' "$id")
        if test "$first" = true; then first=false; else samples="$samples,"; fi
        samples="$samples$stats"
    done
    printf '{"timestamp":"%s","containers":[%s]}\n' "$timestamp" "$samples"
    sleep 2
done
