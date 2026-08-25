# FlexiBilling for Go

Billing and usage metering for Go backends. FlexiBilling tracks named balances,
rates usage, and applies priority rules.

The package follows the [FlexiBilling contract](https://github.com/arterialist/flexibilling).
Interfaces define persistence and cache operations, so a host application can
keep its existing database and transaction model.

## Install

```bash
go get github.com/arterialist/flexibilling-go/flexibilling
```

Amounts use an exact decimal wrapper backed by `math/big.Rat`. Balance and
ledger arithmetic does not use binary floating-point values.

## Quickstart

```go
package main

import (
    billing "github.com/arterialist/flexibilling-go/flexibilling"
)

func main() {
    repository := billing.NewInMemoryBillingRepository()
    repository.Rules = append(repository.Rules, billing.BillingRule{
        Service: "api_request", TargetAsset: "units",
        MetricType: billing.MetricUnits, ConversionRate: billing.MustAmount("1"),
        Priority: 10, IsActive: true,
    })
    repository.UpsertBalance("customer-1", "units", billing.MustAmount("100"))
    record := billing.NewUsageRecord("customer-1", "api_request")
    record.ID = "usage-1"
    units := int64(12)
    record.Units = &units
    repository.Records = append(repository.Records, record)

    service := billing.NewBillingService(repository, billing.NewInMemoryBillingCache())
    _ = service.ProcessRecord(&record)
}
```

Asset and service names are open strings. The constants are neutral
conveniences for examples, not a closed registry.

## Main components

- `Rate` calculates fixed, quantity, duration, and units costs.
- `EvaluateWaterfall` selects the first fundable rule by priority.
- `BillingService` processes records, funds products, charges, refunds, and
  updates cache views.
- `BillingRepository`, `UsageRepository`, and `BillingCache` define host ports.
- `InMemoryBillingRepository` and `InMemoryBillingCache` support tests and
  examples.
- `BillingContext` and `WriteUsageSession` record operation-boundary usage.
- `BillingWorker` processes pending usage records.
- `GetUsageSnapshot` returns used and remaining totals for a period.

## Development

```bash
gofmt -w flexibilling/*.go
go vet ./...
go test ./...
```

The SQLite integration test uses the pure-Go `modernc.org/sqlite` driver. It
checks the public repository and cache interfaces against a persistent database,
including workers, idempotency, refunds, and database reopening.

See [CONTRIBUTING.md](CONTRIBUTING.md) for adapter and release guidance.
