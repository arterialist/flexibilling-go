# FlexiBilling for Go

Provider-agnostic usage metering, multi-asset balances, and configurable
priority waterfalls for Go backends.

The package follows the [language-neutral FlexiBilling contract](https://github.com/arterialist/flexibilling).
Persistence and cache behavior are expressed as interfaces, so host
applications can retain their existing database and transaction model.

## Install

```bash
go get github.com/arterialist/flexibilling-go/flexibilling
```

Amounts use an exact decimal wrapper backed by `math/big.Rat`; binary
floating-point values are not used for balance or ledger arithmetic.

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

## API surface

- `Rate` calculates fixed, quantity, duration, and units costs.
- `EvaluateWaterfall` selects the first fundable rule in priority order.
- `BillingService` processes records, funds products, charges, refunds, and
  synchronizes cache views.
- `BillingRepository`, `UsageRepository`, and `BillingCache` are host ports.
- `InMemoryBillingRepository` and `InMemoryBillingCache` are reference adapters.
- `BillingContext` and `WriteUsageSession` record operation-boundary usage.
- `BillingWorker` drains pending usage records.
- `GetUsageSnapshot` exposes used and remaining totals for a period.

## Development

```bash
gofmt -w flexibilling/*.go
go vet ./...
go test ./...
```

The SQLite integration test uses the current pure-Go `modernc.org/sqlite`
driver to exercise the public repository and cache interfaces against a
persistent database, including workers, idempotency, refunds, and reopening
the database.

See [CONTRIBUTING.md](CONTRIBUTING.md) for adapter and release guidance.
