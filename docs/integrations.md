# Framework integrations

The package does not include framework glue. Bind billing to the host request
layer at the point where the customer is authenticated.

## Request boundary

Resolve the customer from authenticated request state, check the cache or
repository before expensive work, then create a `BillingContext` around the
operation:

```go
customerID := authenticatedCustomerID(request)
if err := billing.RequireBalance(customerID, []string{"units"}, cache); err != nil {
	return err
}

context := billing.BillingContext{
	CustomerID: customerID,
	Service: "report_generation",
	Variant: "standard",
	ReferenceID: requestID,
	Metadata: billing.Metadata{},
}
report, err := generateReport(request.Body)
if err != nil {
	return err
}
context.Report(billing.Zero(), report.DurationSeconds, report.Units, 0, 0, 0, 0)
_, err = billing.WriteUsageSession(context, false, true, usageRepository)
return err
```

The host decides how an insufficient-balance error becomes an HTTP 402 or a
domain-specific response.

## Request wrappers

Go middleware is optional. A handler can call `RequireBalance`,
`BillingService.Charge`, or `WriteUsageSession` directly. This keeps the
integration compatible with `net/http`, Gin, Echo, gRPC, and worker runtimes.

## Background worker

`BillingWorker` drains pending records from `BillingRepository`:

```go
worker := &billing.BillingWorker{Service: service, BatchSize: 50}
result, err := worker.RunOnce()
if err != nil {
	return err
}
log.Printf("processed %d records", result.Processed)
```

Run the worker from a process with a shutdown path. The repository must make
record claiming and balance updates safe under concurrent workers.

## Metrics

The package does not register a metrics library. Count processed, skipped, and
failed records at the worker boundary and export them through the host's
OpenTelemetry, Prometheus, or logging integration.
