#!/usr/bin/env bash
# Starts two record sources over the same seed, one of them slightly wrong,
# then runs a rule against both and shuts everything down.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
rule="${1:-ledger-vs-provider}"
pids=()

cleanup() {
  for pid in "${pids[@]:-}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

go build -o "$root/bin/recon" "$root/cmd/recon"
go build -o "$root/bin/fakesource" "$root/examples/fakesource"

"$root/bin/fakesource" -addr localhost:9101 -seed 42 -records 200 >/dev/null 2>&1 &
pids+=($!)

"$root/bin/fakesource" -addr localhost:9102 -seed 42 -records 200 \
  -drop 40 -shift 33 -restate 25 -duplicate 61 >/dev/null 2>&1 &
pids+=($!)

ready() { (exec 3<>"/dev/tcp/localhost/$1") 2>/dev/null; }

for _ in $(seq 1 50); do
  if ready 9101 && ready 9102; then
    break
  fi
  sleep 0.1
done

set +e
"$root/bin/recon" -config "$root/reconciliation.yaml" -rule "$rule" -since 24h
code=$?
set -e

exit "$code"
