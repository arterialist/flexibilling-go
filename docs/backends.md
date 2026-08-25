# Backend integration

FlexiBilling uses interfaces rather than a required database schema. Keep the
host's existing models and implement the methods in the public ports.

## Ports

- `BillingRepository` handles rules, balances, products, ledger transactions, and queue status.
- `UsageRepository` handles session-created records and usage queries.
- `BillingCache` handles balance snapshots, period statistics, and activity events.
- A transaction factory can provide a transaction for standalone charges and refunds.

Repository methods can receive the transaction value used by the host. Pass a
database transaction or unit-of-work object through that value.

## Minimal implementation shape

```go
type DatabaseRepository struct {
	db *sql.DB
}

func (repository *DatabaseRepository) GetActiveRules(service string) ([]billing.BillingRule, error) {
	return loadActiveRules(repository.db, service)
}

// Implement the remaining BillingRepository and UsageRepository methods.
```

The compiler checks the complete port before it is passed to
`BillingService`.

## In-memory adapters

`InMemoryBillingRepository` and `InMemoryBillingCache` are useful for tests,
examples, and local experiments. They do not persist across processes and do
not provide cross-process locking.

## SQL databases

The package does not choose a SQL client. Map record fields directly to the host
schema. Store `Amount` values as exact decimal or numeric values. Store
`EventMetadata` as JSON when the database supports it, and index the customer,
status, service, and created-at fields used by the worker and usage queries.
