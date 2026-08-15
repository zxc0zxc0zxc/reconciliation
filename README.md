# reconciliation

[![CI](https://github.com/zxc0zxc0zxc/reconciliation/actions/workflows/ci.yml/badge.svg)](https://github.com/zxc0zxc0zxc/reconciliation/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zxc0zxc0zxc/reconciliation.svg)](https://pkg.go.dev/github.com/zxc0zxc0zxc/reconciliation)
[![Go Report Card](https://goreportcard.com/badge/github.com/zxc0zxc0zxc/reconciliation)](https://goreportcard.com/report/github.com/zxc0zxc0zxc/reconciliation)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A source-agnostic reconciliation engine.

Two systems claim to hold the same truth — a ledger and a provider, a database and an
API, yesterday's export and today's. This tool pulls a time window from both, matches
records by a correlation key, and reports exactly where they disagree.

Everything specific to a source lives behind **one gRPC method**. The engine itself is a
pure function over two slices of records: no I/O, no clock, no database.

## Design

```
   SOURCES                       ENGINE                        OUTPUT

 ┌────────────┐
 │   Ledger   │──┐
 └────────────┘  │   ┌──────────┐   ╔═══════════════╗   ┌────────────┐
                 ├──▶│ Collector│──▶║   MATCHING    ║──▶│Discrepancy │
 ┌────────────┐  │   │ side A/B │   ║ CLASSIFICATION║   │  report    │
 │  Provider  │──┘   └──────────┘   ╚═══════════════╝   └────────────┘
 └────────────┘            ▲               ▲
                           │               │
                    GetRecords(from,to)   Rule (YAML)
```

| Package | Role |
|---|---|
| `recon` | the engine: matching, classification, severity. No dependencies. |
| `source` | gRPC client for `RecordSource`, paging and model conversion |
| `runner` | fetches both sides concurrently, picks the as-of instant, calls the engine |
| `config` | YAML sources and rules, validated on load |
| `report` | text and JSON rendering |
| `cmd/recon` | CLI |
| `examples/fakesource` | a source server that can be told to be wrong on purpose |

## The one interface

```proto
service RecordSource {
  rpc GetRecords(GetRecordsRequest) returns (GetRecordsResponse);
}

message Record {
  string id       = 1;  // native id in the source system
  string key      = 2;  // correlation key across sources
  int64  amount   = 3;  // minor units, never floats
  string currency = 4;
  google.protobuf.Timestamp occurred_at = 5;
  string status   = 6;
  map<string, string> attributes = 7;  // anything source-specific
}
```

Adding a source means implementing that server, not touching the engine. Responses carry
an `as_of` instant; a run uses the earlier of the two, so a lagging source never makes
records look settled before they are.

## Rules

A rule declares how two sides are compared. Partial set — the useful ones first.

| Field | Meaning |
|---|---|
| `key` | correlation is on `Record.key`; sources decide what fills it |
| `mode` | `item` (record by record) or `balance` (totals per currency) |
| `amount_tolerance` | allowed absolute difference, minor units |
| `in_flight_window` | records younger than this are excluded from both sides |
| `require_status_match` | a matching pair with conflicting statuses is still a finding |
| `thresholds` | amount at stake that makes a finding medium, high, critical |

```yaml
sources:
  ledger:
    address: localhost:9101
  provider:
    address: localhost:9102
    page_size: 500

rules:
  - name: ledger-vs-provider
    sides: [ledger, provider]
    mode: item
    amount_tolerance: 0
    in_flight_window: 5m
    require_status_match: true
    thresholds: {medium: 1000, high: 100000, critical: 10000000}
```

## Discrepancy classes

| Class | Meaning |
|---|---|
| `MISSING_IN_A` | present on side B only |
| `MISSING_IN_B` | present on side A only |
| `AMOUNT_MISMATCH` | same key, amounts differ beyond tolerance |
| `CURRENCY_MISMATCH` | same key, different currencies |
| `STATUS_MISMATCH` | same key and amount, conflicting statuses |
| `DUPLICATE` | one key carried by several records on one side |

Severity comes from the amount at stake, not from the class.

## Run it

```
make demo
```

Two sources are started over the same seed; the second one drops, restates, duplicates
and shifts some records. Then the rule runs against both:

```
rule     ledger-vs-provider (ledger vs provider)
window   2026-08-14T16:55:06Z .. 2026-08-15T16:55:06Z
as of    2026-08-15T16:55:06Z
scanned  200 / 198 records, 10 in flight
matched  175
findings 20 (critical 0, high 10, medium 10, low 0)

KEY       CLASS            SEVERITY  DELTA   CURRENCY  DETAILS
op-00000  MISSING_IN_B     medium    72405   EUR       present in ledger only
op-00025  STATUS_MISMATCH  high      0       USD       settled vs pending
op-00033  AMOUNT_MISMATCH  medium    -1337   USD       343596 vs 344933
op-00061  DUPLICATE        high      482420  USD       2 records on side B
```

The CLI exits `0` on a clean run, `1` when there are findings, `2` on failure, so it
drops into a pipeline as is:

```
recon -config reconciliation.yaml -rule ledger-vs-provider -since 24h
recon -config reconciliation.yaml -rule ledger-vs-provider-totals -format json
recon -config reconciliation.yaml -list
```

## Build

```
make build     # recon and fakesource into bin/
make test      # unit and gRPC round-trip tests
make race      # the same under the race detector
make cover     # total coverage
make lint      # golangci-lint
make proto     # regenerate gen/ from proto/ (protoc, protoc-gen-go, protoc-gen-go-grpc)
```

Go 1.25+. Generated code is committed, so the proto toolchain is only needed to change
the contract; CI regenerates it and fails on a diff.

## Scope

This is the comparison core, kept deliberately small. A production deployment around it
adds scheduling with a distributed lease, persistence and run history, alert routing, and
a remediation path with human approval — none of which change how the engine decides that
two records disagree.

## License

MIT
