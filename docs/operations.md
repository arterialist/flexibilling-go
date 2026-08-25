# Operations

## Before production

1. Define and review active rules for each billable service.
2. Seed product mappings and verify external product identifiers.
3. Add indexes for balances and pending usage records.
4. Use a transaction-aware repository for balance deductions and ledger writes.
5. Configure a durable cache if fast balance checks or period views need it.
6. Run workers with a bounded batch size and a shutdown path.
7. Export worker outcomes and alert on repeated failed records.
8. Test duplicate payment and usage delivery before launch.

## Cache consistency

The database is authoritative. Cache writes update derived views after a
successful balance operation. Refresh a customer's cache after eviction or
deployment if the host starts with an empty cache.

If the cache is unavailable, choose an application-specific fallback. Do not
silently treat a missing cache as proof that a customer can transact.

## Transactions and retries

Keep balance locking, deduction, ledger insertion, and usage status updates in
one database transaction. Do not acknowledge a queue message before commit.
Leave a record pending after an unexpected error so a later worker can retry.

## Product grants

`FundCustomer` accepts external product IDs and a payment reference. It checks
for an existing ledger transaction before applying each active mapping.

- `ProductTopUp` adds to the current balance;
- `ProductMonthlyQuota` replaces it with the configured grant.

Keep the payment provider's event identifier stable across retries.

## Security and privacy

- Treat customer IDs, payment references, and event metadata as application data.
- Do not put secrets in `EventMetadata` or cache activity events.
- Authorize balance, usage, and ledger reads for the owning customer or operator.
- Use TLS for database and cache connections outside local development.
