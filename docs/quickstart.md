# Quickstart

This guide uses the in-memory adapters. The service calls stay the same when
you implement the repository and cache interfaces for a real backend.

## 1. Install

```bash
mkdir billing-example
cd billing-example
go mod init example.com/billing
go get github.com/arterialist/flexibilling-go/flexibilling
```

## 2. Define a rule and balance

```go
repository := billing.NewInMemoryBillingRepository()
repository.Rules = append(repository.Rules, billing.BillingRule{
	Service: "api_request", TargetAsset: "units",
	MetricType: billing.MetricUnits, ConversionRate: billing.MustAmount("1"),
	Priority: 10, IsActive: true,
})
_, _ = repository.UpsertBalance("customer-001", "units", billing.MustAmount("100"))
cache := billing.NewInMemoryBillingCache()
service := billing.NewBillingService(repository, cache)
```

## 3. Process usage

```go
record := billing.NewUsageRecord("customer-001", "api_request")
record.ID = "usage-1"
units := int64(12)
record.Units = &units
repository.Records = append(repository.Records, record)

if err := service.ProcessRecord(&record); err != nil {
	panic(err)
}
fmt.Println(cache.GetBalances("customer-001"))
```

The service selects an active rule by priority, calculates the cost, deducts
the selected balance, writes a ledger transaction, updates the cache, and
marks the record processed.

## 4. Track an operation session

```go
context := billing.BillingContext{
	CustomerID: "customer-001",
	Service: "api_request",
	Variant: "standard",
	ReferenceID: "request-1002",
	Metadata: billing.Metadata{},
}
context.Report(billing.Zero(), billing.MustAmount("0.45"), 24, 0, 0, 0, 0)
record, err := billing.WriteUsageSession(context, false, true, usageRepository)
```

The session writes `DurationSeconds` to the record and mirrors it into
`EventMetadata["duration_seconds"]` when that key is absent. Set
`writeOnException` to `false` to skip failed operations.

## 5. Use a custom backend

Implement `BillingRepository`, `UsageRepository`, and `BillingCache` for the
host database and cache. The in-memory adapters are not required in production.
