# Concepts

## Core objects

| Object | Purpose |
| --- | --- |
| `CustomerBalance` | Current amount of one named asset for a customer. |
| `BillingRule` | Maps a service and metric to an asset and conversion rate. |
| `UsageRecord` | A billable event waiting to be rated and processed. |
| `BalanceTransaction` | Immutable ledger movement from funding, usage, or refunds. |
| `BillingProduct` | External product identifier mapped to a balance grant. |
| `BillingCache` | Materialized balances, period counters, and activity feed. |

Customer IDs, service names, asset names, variants, and reference IDs are
application-defined strings.

## Rating metrics

| Metric | Cost calculation |
| --- | --- |
| `MetricFixed` | `ConversionRate` once per record. |
| `MetricQuantity` | `Quantity` multiplied by the rate. |
| `MetricDuration` | `DurationSeconds` multiplied by the rate. |
| `MetricUnits` | `Units`, or input plus output units, multiplied by the rate. |

Duration can come from `UsageRecord.DurationSeconds` or
`EventMetadata["duration_seconds"]`. Metadata takes precedence, which lets a
host record an authoritative elapsed value without changing its model.

## The priority waterfall

The engine sorts active rules by ascending `Priority`:

1. It skips a rule whose metadata filter does not match.
2. It calculates a positive cost.
3. It selects the first target asset with enough balance.
4. It reports no billable usage when no rule produces a positive cost.
5. It reports insufficient funds when costs exist but no rule can be funded.

This supports a primary quota with a prepaid fallback without reserving either
asset name.

## Transactions and idempotency

The repository owns the transaction that locks a balance, deducts the amount,
inserts the ledger entry, and updates record status. `FundCustomer` is
idempotent on `PaymentReference`, so a retried payment event does not grant a
product twice.

`Charge` and `Refund` can use a caller-owned transaction or a configured
transaction factory.

## Queue states

The worker marks expected outcomes explicitly:

- processed records become `StatusProcessed`;
- records without billable usage become `StatusSkipped`;
- insufficient funds and other expected billing failures become `StatusFailed`;
- unexpected errors remain pending for a later retry.
