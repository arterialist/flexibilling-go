# FlexiBilling for Go

[![CI](https://github.com/arterialist/flexibilling-go/actions/workflows/ci.yaml/badge.svg)](https://github.com/arterialist/flexibilling-go/actions/workflows/ci.yaml)
[![pkg.go.dev](https://pkg.go.dev/badge/github.com/arterialist/flexibilling-go/flexibilling.svg)](https://pkg.go.dev/github.com/arterialist/flexibilling-go/flexibilling)
[![Docs](https://img.shields.io/badge/docs-online-blue.svg)](https://arterialist.github.io/flexibilling-go/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

FlexiBilling is a billing engine for Go backends. It tracks
named balances, rates usage, applies priority rules, writes ledger entries, and
processes pending usage records.

The package leaves storage, web frameworks, caches, and payment providers to the
host application. Implement the exported repository and cache interfaces for the
host's storage and transaction boundaries, or use the included in-memory adapters.

## Install

```bash
go get github.com/arterialist/flexibilling-go/flexibilling
```

Amounts use an exact decimal wrapper backed by `math/big.Rat`. Balance and
ledger calculations do not use binary floating-point values.

## Quickstart

```go
package main

import billing "github.com/arterialist/flexibilling-go/flexibilling"

func main() {
	repository := billing.NewInMemoryBillingRepository()
	repository.Rules = append(repository.Rules, billing.BillingRule{
		Service: "api_request", TargetAsset: "units",
		MetricType: billing.MetricUnits, ConversionRate: billing.MustAmount("1"),
		Priority: 10, IsActive: true,
	})
	_, _ = repository.UpsertBalance("customer-1", "units", billing.MustAmount("100"))

	record := billing.NewUsageRecord("customer-1", "api_request")
	record.ID = "usage-1"
	units := int64(12)
	record.Units = &units
	repository.Records = append(repository.Records, record)

	service := billing.NewBillingService(repository, billing.NewInMemoryBillingCache())
	_ = service.ProcessRecord(&record)
}
```

Asset and service names are open strings. The constants are neutral helpers for
examples, not a closed registry.

## Usage sessions

Use `BillingContext` and `WriteUsageSession` when an operation discovers usage:

```go
context := billing.BillingContext{
	CustomerID:  "customer-1",
	Service:     "api_request",
	Variant:     "standard",
	ReferenceID: "request-123",
	Metadata:    billing.Metadata{},
}
context.Report(billing.Zero(), billing.MustAmount("0.45"), 12, 0, 0, 0, 0)
record, err := billing.WriteUsageSession(context, false, true, usageRepository)
```

`DurationSeconds` is stored on the usage record and mirrored into
`EventMetadata["duration_seconds"]` when the caller has not already set it.
Set `writeOnException` to `false` to skip failed operations.

## Included components

- `BillingService` funds accounts, rates usage, charges, refunds, and updates cache views.
- `BillingRepository`, `UsageRepository`, and `BillingCache` define backend ports.
- `Rate` and `EvaluateWaterfall` calculate costs and select fundable rules.
- `BillingContext` and `WriteUsageSession` record operation-boundary usage.
- `BillingWorker` processes pending records and records each outcome.
- The in-memory adapters are useful in tests and local programs.

## Documentation

Read the [Go documentation](https://arterialist.github.io/flexibilling-go/)
for the quickstart, concepts, backend ports, integrations, operations, and
release process. The generated API reference is also available on
[pkg.go.dev](https://pkg.go.dev/github.com/arterialist/flexibilling-go/flexibilling).

## Development

```bash
gofmt -d flexibilling examples/basic
go vet ./...
go test ./...
go test -race ./...
uvx --with mkdocs-material mkdocs build --strict
```

The Go module is indexed automatically from its public version tags.

## License

Apache-2.0. See [LICENSE](LICENSE).
