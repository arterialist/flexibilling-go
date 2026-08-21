# Contributing

## Setup

Install Go 1.27 or newer and run:

```bash
gofmt -w flexibilling/*.go
go vet ./...
go test ./...
```

The tests cover exact-decimal rating, waterfall fallback, distinct domain
errors, ledger writes, payment idempotency, usage sessions, charge/refund,
worker processing, and period snapshots.

`flexibilling/sqlite_integration_test.go` is the minimal real-backend
conformance test. The SQLite driver is used only by tests; Go modules do not
have a separate dev-dependency section, so it remains in `go.mod` while the
library API stays database-agnostic.

## Backend adapters

Implement `BillingRepository` for balances, rules, products, ledger entries,
and usage queue state. Implement `BillingCache` for materialized balance and
period views. The interface calls are synchronous and caller-owned: invoke the
service inside the host transaction or unit of work.

Balance mutation and its ledger entry must share that transaction. Cache writes
are derived views and must not change the billing decision.

## Releases

Create a GitHub release with a semver tag. After a Go module proxy and release
tag are available, the module can be consumed directly with `go get`.
