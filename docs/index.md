# FlexiBilling for Go

FlexiBilling is a billing engine for Go services. It manages named balances,
rates usage, applies priority rules, writes ledger transactions, grants
products idempotently, updates cache views, and processes a pending usage queue.

The package keeps storage, transactions, web frameworks, caches, and payment
providers in the host application. Implement the exported repository and cache
interfaces for the host's data model, or use the in-memory adapters for tests
and small programs.

## Install

```bash
go get github.com/arterialist/flexibilling-go/flexibilling
```

Amounts use an exact decimal wrapper backed by `math/big.Rat`.

## Guides

- [Quickstart](quickstart.md) creates rules, funds a customer, and processes usage.
- [Concepts](concepts.md) explains balances, metrics, rules, waterfalls, and ledger entries.
- [Backend integration](backends.md) shows the repository, usage, and cache interfaces.
- [Framework integrations](integrations.md) covers operation boundaries and workers.
- [Operations](operations.md) covers transactions, retries, cache behavior, and production checks.
- [Development and releases](development.md) covers local checks, CI, docs, and module indexing.

## Design guarantees

1. Billing decisions do not depend on a storage provider.
2. Asset and service names are application-defined strings.
3. Exact decimal values are used for balances, rates, and ledger amounts.
4. Cache and observability failures do not change the billing decision.
5. A host owns the transaction used for balance deductions and ledger writes.
