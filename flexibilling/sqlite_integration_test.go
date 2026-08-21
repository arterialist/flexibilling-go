package flexibilling

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteBackend struct {
	db *sql.DB
}

func newSQLiteBackend(t *testing.T, filename string) *sqliteBackend {
	t.Helper()
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS rules (
			id TEXT PRIMARY KEY, service TEXT NOT NULL, active INTEGER NOT NULL,
			priority INTEGER NOT NULL, payload TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS products (
			external_product_id TEXT PRIMARY KEY, active INTEGER NOT NULL, payload TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS balances (
			customer_id TEXT NOT NULL, asset_type TEXT NOT NULL, amount TEXT NOT NULL,
			PRIMARY KEY (customer_id, asset_type)
		);
		CREATE TABLE IF NOT EXISTS usage_records (
			id TEXT PRIMARY KEY, customer_id TEXT NOT NULL, service TEXT NOT NULL,
			status TEXT NOT NULL, reference_id TEXT, created_at TEXT NOT NULL, payload TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id TEXT NOT NULL,
			transaction_type TEXT NOT NULL, payment_reference TEXT,
			source_usage_id TEXT, payload TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS cache_balances (
			customer_id TEXT NOT NULL, asset_type TEXT NOT NULL, amount TEXT NOT NULL,
			PRIMARY KEY (customer_id, asset_type)
		);
		CREATE TABLE IF NOT EXISTS cache_stats (
			customer_id TEXT NOT NULL, month TEXT NOT NULL, metric TEXT NOT NULL,
			amount TEXT NOT NULL, PRIMARY KEY (customer_id, month, metric)
		);
		CREATE TABLE IF NOT EXISTS cache_feed (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id TEXT NOT NULL, payload TEXT NOT NULL
		);
	`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return &sqliteBackend{db: db}
}

func (backend *sqliteBackend) close() {
	_ = backend.db.Close()
}

func (backend *sqliteBackend) seedRule(t *testing.T, rule BillingRule) {
	t.Helper()
	payload, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" {
		rule.ID = "rule-1"
	}
	if _, err := backend.db.Exec(
		"INSERT INTO rules (id, service, active, priority, payload) VALUES (?, ?, ?, ?, ?)",
		rule.ID, rule.Service, boolInt(rule.IsActive), rule.Priority, payload,
	); err != nil {
		t.Fatal(err)
	}
}

func (backend *sqliteBackend) seedProduct(t *testing.T, product BillingProduct) {
	t.Helper()
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.Exec(
		"INSERT INTO products (external_product_id, active, payload) VALUES (?, ?, ?)",
		product.ExternalProductID, boolInt(product.IsActive), payload,
	); err != nil {
		t.Fatal(err)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (backend *sqliteBackend) transactionCount() int {
	var count int
	if err := backend.db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&count); err != nil {
		panic(err)
	}
	return count
}

func marshal(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func unmarshal[T any](payload string) T {
	var value T
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		panic(err)
	}
	return value
}

func (backend *sqliteBackend) getRecords(customerID string) []UsageRecord {
	rows, err := backend.db.Query("SELECT payload FROM usage_records WHERE customer_id = ?", customerID)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []UsageRecord{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		result = append(result, unmarshal[UsageRecord](payload))
	}
	return result
}

func (backend *sqliteBackend) getAllRecords() []UsageRecord {
	rows, err := backend.db.Query("SELECT payload FROM usage_records")
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []UsageRecord{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		result = append(result, unmarshal[UsageRecord](payload))
	}
	return result
}

func (backend *sqliteBackend) updateRecord(record UsageRecord) error {
	_, err := backend.db.Exec(
		"UPDATE usage_records SET status = ?, payload = ? WHERE id = ?",
		record.BillingStatus, marshal(record), record.ID,
	)
	return err
}

func (backend *sqliteBackend) GetActiveRules(service string) []BillingRule {
	rows, err := backend.db.Query("SELECT payload FROM rules WHERE service = ? AND active = 1 ORDER BY priority", service)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []BillingRule{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		result = append(result, unmarshal[BillingRule](payload))
	}
	return result
}

func (backend *sqliteBackend) GetCustomerBalances(customerID string) []CustomerBalance {
	rows, err := backend.db.Query("SELECT asset_type, amount FROM balances WHERE customer_id = ?", customerID)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []CustomerBalance{}
	for rows.Next() {
		var assetType, value string
		if err := rows.Scan(&assetType, &value); err != nil {
			panic(err)
		}
		result = append(result, CustomerBalance{CustomerID: customerID, AssetType: assetType, Amount: MustAmount(value)})
	}
	return result
}

func (backend *sqliteBackend) UpsertBalance(customerID, assetType string, value Amount) (CustomerBalance, error) {
	_, err := backend.db.Exec(`
		INSERT INTO balances (customer_id, asset_type, amount) VALUES (?, ?, ?)
		ON CONFLICT(customer_id, asset_type) DO UPDATE SET amount = excluded.amount
	`, customerID, assetType, value.String())
	return CustomerBalance{CustomerID: customerID, AssetType: assetType, Amount: value}, err
}

func (backend *sqliteBackend) DecrementBalance(customerID, assetType string, deduction Amount) (Amount, error) {
	var value string
	err := backend.db.QueryRow("SELECT amount FROM balances WHERE customer_id = ? AND asset_type = ?", customerID, assetType).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		value = "0"
	} else if err != nil {
		return Zero(), err
	}
	current := MustAmount(value)
	if current.Cmp(deduction) < 0 {
		return Zero(), &BillingError{Kind: ErrorInsufficientFunds, CustomerID: customerID, Service: "charge", Message: "insufficient funds"}
	}
	next := current.Sub(deduction)
	_, err = backend.db.Exec("UPDATE balances SET amount = ? WHERE customer_id = ? AND asset_type = ?", next.String(), customerID, assetType)
	return next, err
}

func (backend *sqliteBackend) IncrementBalance(customerID, assetType string, addition Amount) (Amount, error) {
	var value string
	err := backend.db.QueryRow("SELECT amount FROM balances WHERE customer_id = ? AND asset_type = ?", customerID, assetType).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		value = "0"
	} else if err != nil {
		return Zero(), err
	}
	next := MustAmount(value).Add(addition)
	_, err = backend.db.Exec(`
		INSERT INTO balances (customer_id, asset_type, amount) VALUES (?, ?, ?)
		ON CONFLICT(customer_id, asset_type) DO UPDATE SET amount = excluded.amount
	`, customerID, assetType, next.String())
	return next, err
}

func (backend *sqliteBackend) CreateTransaction(data BalanceTransactionCreate) (BalanceTransaction, error) {
	transaction := BalanceTransaction{
		CustomerID: data.CustomerID, AssetType: data.AssetType, Amount: data.Amount,
		BalanceAfter: data.BalanceAfter, TransactionType: data.TransactionType,
		SourceUsageID: data.SourceUsageID, PaymentReference: data.PaymentReference,
		Description: data.Description, CreatedAt: time.Now().UTC(),
	}
	result, err := backend.db.Exec(
		"INSERT INTO transactions (customer_id, transaction_type, payment_reference, source_usage_id, payload) VALUES (?, ?, ?, ?, ?)",
		transaction.CustomerID, transaction.TransactionType, transaction.PaymentReference,
		transaction.SourceUsageID, marshal(transaction),
	)
	if err != nil {
		return BalanceTransaction{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return BalanceTransaction{}, err
	}
	transaction.ID = RecordId(strconv.FormatInt(id, 10))
	_, err = backend.db.Exec("UPDATE transactions SET payload = ? WHERE id = ?", marshal(transaction), id)
	return transaction, err
}

func (backend *sqliteBackend) GetTransactionForUsage(referenceID, service, customerID string) (BalanceTransaction, bool) {
	ids := map[string]bool{}
	for _, record := range backend.getRecords(customerID) {
		if record.ReferenceID == referenceID && record.Service == service {
			ids[record.ID] = true
		}
	}
	rows, err := backend.db.Query("SELECT payload FROM transactions WHERE customer_id = ? AND transaction_type = ? ORDER BY id DESC", customerID, TransactionUsage)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		transaction := unmarshal[BalanceTransaction](payload)
		if ids[transaction.SourceUsageID] {
			return transaction, true
		}
	}
	return BalanceTransaction{}, false
}

func (backend *sqliteBackend) GetTransactionByReference(paymentReference string) (BalanceTransaction, bool) {
	var payload string
	err := backend.db.QueryRow("SELECT payload FROM transactions WHERE payment_reference = ? ORDER BY id LIMIT 1", paymentReference).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return BalanceTransaction{}, false
	}
	if err != nil {
		panic(err)
	}
	return unmarshal[BalanceTransaction](payload), true
}

func (backend *sqliteBackend) GetProductsForExternalIDs(productIDs []string) []BillingProduct {
	result := []BillingProduct{}
	for _, productID := range productIDs {
		var payload string
		err := backend.db.QueryRow("SELECT payload FROM products WHERE external_product_id = ? AND active = 1", productID).Scan(&payload)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			panic(err)
		}
		result = append(result, unmarshal[BillingProduct](payload))
	}
	return result
}

func (backend *sqliteBackend) GetPendingRecords(limit int) []UsageRecord {
	rows, err := backend.db.Query("SELECT payload FROM usage_records WHERE status = ? ORDER BY created_at LIMIT ?", StatusPending, limit)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []UsageRecord{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		result = append(result, unmarshal[UsageRecord](payload))
	}
	return result
}

func (backend *sqliteBackend) markRecord(recordID, status, message string) error {
	var payload string
	if err := backend.db.QueryRow("SELECT payload FROM usage_records WHERE id = ?", recordID).Scan(&payload); err != nil {
		return err
	}
	record := unmarshal[UsageRecord](payload)
	record.BillingStatus = status
	record.BillingErrorMessage = message
	return backend.updateRecord(record)
}

func (backend *sqliteBackend) MarkRecordProcessed(recordID string) error {
	return backend.markRecord(recordID, StatusProcessed, "")
}

func (backend *sqliteBackend) MarkRecordFailed(recordID, message string) error {
	return backend.markRecord(recordID, StatusFailed, message)
}

func (backend *sqliteBackend) MarkRecordSkipped(recordID string) error {
	return backend.markRecord(recordID, StatusSkipped, "")
}

func (backend *sqliteBackend) Create(data UsageRecordCreate) (*UsageRecord, error) {
	var count int
	if err := backend.db.QueryRow("SELECT COUNT(*) FROM usage_records").Scan(&count); err != nil {
		return nil, err
	}
	record := UsageRecord{
		CustomerID: data.CustomerID, Service: data.Service, Variant: data.Variant,
		ID: RecordId(strconv.FormatInt(int64(count+1), 10)), ReferenceID: data.ReferenceID,
		Quantity: data.Quantity, DurationSeconds: data.DurationSeconds, Units: data.Units,
		InputUnits: data.InputUnits, OutputUnits: data.OutputUnits, CachedUnits: data.CachedUnits,
		BillingStatus: data.BillingStatus, BillingErrorMessage: data.BillingErrorMessage,
		EventMetadata: data.EventMetadata, CreatedAt: time.Now().UTC(),
	}
	record.ID = "usage-" + record.ID
	_, err := backend.db.Exec(
		"INSERT INTO usage_records (id, customer_id, service, status, reference_id, created_at, payload) VALUES (?, ?, ?, ?, ?, ?, ?)",
		record.ID, record.CustomerID, record.Service, record.BillingStatus, record.ReferenceID,
		record.CreatedAt.Format(time.RFC3339Nano), marshal(record),
	)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (backend *sqliteBackend) GetByCustomer(customerID string, skip, limit int) []UsageRecord {
	records := backend.getRecords(customerID)
	sort.SliceStable(records, func(left, right int) bool { return records[left].CreatedAt.After(records[right].CreatedAt) })
	if skip >= len(records) {
		return []UsageRecord{}
	}
	end := skip + limit
	if end > len(records) {
		end = len(records)
	}
	return records[skip:end]
}

func (backend *sqliteBackend) GetUsageSummary(customerID string, from, to *time.Time) []UsageSummary {
	groups := map[string][]UsageRecord{}
	for _, record := range backend.getRecords(customerID) {
		if from != nil && record.CreatedAt.Before(*from) || to != nil && record.CreatedAt.After(*to) {
			continue
		}
		key := record.Service + "\x00" + record.Variant
		groups[key] = append(groups[key], record)
	}
	result := []UsageSummary{}
	for key, records := range groups {
		parts := strings.SplitN(key, "\x00", 2)
		result = append(result, UsageSummary{Service: parts[0], Variant: parts[1], UsageCount: int64(len(records)), TotalQuantity: sumAmounts(records, func(record UsageRecord) *Amount { return record.Quantity }), TotalDurationSeconds: sumAmounts(records, func(record UsageRecord) *Amount { return record.DurationSeconds }), TotalUnits: sumInts(records, func(record UsageRecord) *int64 { return record.Units }), TotalInputUnits: sumInts(records, func(record UsageRecord) *int64 { return record.InputUnits }), TotalOutputUnits: sumInts(records, func(record UsageRecord) *int64 { return record.OutputUnits }), TotalCachedUnits: sumInts(records, func(record UsageRecord) *int64 { return record.CachedUnits })})
	}
	return result
}

func (backend *sqliteBackend) GetUsageRecords(customerID string, from, to *time.Time, service string, limit, offset int) ([]UsageRecord, int) {
	records := backend.GetByCustomer(customerID, 0, len(backend.getRecords(customerID)))
	filtered := records[:0]
	for _, record := range records {
		if service != "" && record.Service != service || from != nil && record.CreatedAt.Before(*from) || to != nil && record.CreatedAt.After(*to) {
			continue
		}
		filtered = append(filtered, record)
	}
	total := len(filtered)
	if offset >= total {
		return []UsageRecord{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total
}

func (backend *sqliteBackend) SetBalances(customerID string, balances map[string]Amount) error {
	if _, err := backend.db.Exec("DELETE FROM cache_balances WHERE customer_id = ?", customerID); err != nil {
		return err
	}
	for asset, value := range balances {
		if err := backend.UpdateSingleBalance(customerID, asset, value); err != nil {
			return err
		}
	}
	return nil
}

func (backend *sqliteBackend) UpdateSingleBalance(customerID, assetType string, value Amount) error {
	_, err := backend.db.Exec(`
		INSERT INTO cache_balances (customer_id, asset_type, amount) VALUES (?, ?, ?)
		ON CONFLICT(customer_id, asset_type) DO UPDATE SET amount = excluded.amount
	`, customerID, assetType, value.String())
	return err
}

func (backend *sqliteBackend) GetBalances(customerID string) map[string]string {
	rows, err := backend.db.Query("SELECT asset_type, amount FROM cache_balances WHERE customer_id = ?", customerID)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var asset, value string
		if err := rows.Scan(&asset, &value); err != nil {
			panic(err)
		}
		result[asset] = value
	}
	canTransact := false
	for asset, value := range result {
		if asset != "can_transact" && MustAmount(value).GreaterThanZero() {
			canTransact = true
		}
	}
	if canTransact {
		result["can_transact"] = "1"
	} else {
		result["can_transact"] = "0"
	}
	return result
}

func (backend *sqliteBackend) CanTransact(customerID string) bool {
	return backend.GetBalances(customerID)["can_transact"] == "1"
}

func (backend *sqliteBackend) GetAssetAmount(customerID, assetType string) (Amount, bool) {
	var value string
	if err := backend.db.QueryRow("SELECT amount FROM cache_balances WHERE customer_id = ? AND asset_type = ?", customerID, assetType).Scan(&value); errors.Is(err, sql.ErrNoRows) {
		return Zero(), false
	} else if err != nil {
		panic(err)
	}
	return MustAmount(value), true
}

func (backend *sqliteBackend) DeleteBalances(customerID string) error {
	_, err := backend.db.Exec("DELETE FROM cache_balances WHERE customer_id = ?", customerID)
	return err
}

func (backend *sqliteBackend) incrementStat(customerID, month, metric string, value Amount) error {
	if value.IsZero() {
		return nil
	}
	var current string
	err := backend.db.QueryRow("SELECT amount FROM cache_stats WHERE customer_id = ? AND month = ? AND metric = ?", customerID, month, metric).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = "0"
	} else if err != nil {
		return err
	}
	_, err = backend.db.Exec(`
		INSERT INTO cache_stats (customer_id, month, metric, amount) VALUES (?, ?, ?, ?)
		ON CONFLICT(customer_id, month, metric) DO UPDATE SET amount = excluded.amount
	`, customerID, month, metric, MustAmount(current).Add(value).String())
	return err
}

func (backend *sqliteBackend) IncrementStats(customerID, month string, stats BillingStats) error {
	if err := backend.incrementStat(customerID, month, "total_usage_count", AmountFromInt(stats.UsageCount)); err != nil {
		return err
	}
	if err := backend.incrementStat(customerID, month, "total_quantity", stats.Quantity); err != nil {
		return err
	}
	if err := backend.incrementStat(customerID, month, "total_spend", stats.Spend); err != nil {
		return err
	}
	for name, value := range stats.Custom {
		if err := backend.incrementStat(customerID, month, "total_custom:"+name, value); err != nil {
			return err
		}
	}
	return nil
}

func (backend *sqliteBackend) GetStats(customerID, month string) map[string]string {
	rows, err := backend.db.Query("SELECT metric, amount FROM cache_stats WHERE customer_id = ? AND month = ?", customerID, month)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var metric, value string
		if err := rows.Scan(&metric, &value); err != nil {
			panic(err)
		}
		result[metric] = value
	}
	return result
}

func (backend *sqliteBackend) PushFeedEvent(customerID string, event ActivityEvent) error {
	_, err := backend.db.Exec("INSERT INTO cache_feed (customer_id, payload) VALUES (?, ?)", customerID, marshal(event))
	return err
}

func (backend *sqliteBackend) GetFeed(customerID string, limit int) []ActivityEvent {
	rows, err := backend.db.Query("SELECT payload FROM cache_feed WHERE customer_id = ? ORDER BY id DESC LIMIT ?", customerID, limit)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	result := []ActivityEvent{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			panic(err)
		}
		result = append(result, unmarshal[ActivityEvent](payload))
	}
	return result
}

func (backend *sqliteBackend) DeleteCustomerCache(customerID string) error {
	if _, err := backend.db.Exec("DELETE FROM cache_balances WHERE customer_id = ?", customerID); err != nil {
		return err
	}
	if _, err := backend.db.Exec("DELETE FROM cache_stats WHERE customer_id = ?", customerID); err != nil {
		return err
	}
	_, err := backend.db.Exec("DELETE FROM cache_feed WHERE customer_id = ?", customerID)
	return err
}

func testUsageData(customerID string, units int64) UsageRecordCreate {
	return UsageRecordCreate{CustomerID: customerID, Service: "api_request", Variant: "default", Units: int64Ptr(units), BillingStatus: StatusPending}
}

func testSQLiteRule() BillingRule {
	return BillingRule{Service: "api_request", TargetAsset: "units", MetricType: MetricUnits, ConversionRate: MustAmount("1"), Priority: 10, IsActive: true, ID: "rule-1"}
}

func TestPublicPortsWorkWithPersistentSQLite(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "billing.sqlite")
	backend := newSQLiteBackend(t, filename)
	defer backend.close()
	backend.seedRule(t, testSQLiteRule())
	backend.seedProduct(t, BillingProduct{ExternalProductID: "plan-standard", AssetType: "units", Amount: MustAmount("100"), Strategy: ProductTopUp, IsActive: true})
	if _, err := backend.UpsertBalance("customer-1", "units", MustAmount("100")); err != nil {
		t.Fatal(err)
	}

	service := NewBillingService(backend, backend)
	first, err := backend.Create(testUsageData("customer-1", 4))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessRecord(first); err != nil {
		t.Fatal(err)
	}
	if backend.GetBalances("customer-1")["units"] != "96" || backend.transactionCount() != 1 {
		t.Fatalf("first process did not persist: balances=%v transactions=%d", backend.GetBalances("customer-1"), backend.transactionCount())
	}

	if funded, err := service.FundCustomer("customer-1", []string{"plan-standard"}, "payment-1"); err != nil || !funded {
		t.Fatalf("first funding = %v, %v", funded, err)
	}
	if funded, err := service.FundCustomer("customer-1", []string{"plan-standard"}, "payment-1"); err != nil || funded {
		t.Fatalf("repeated funding = %v, %v", funded, err)
	}
	if backend.GetBalances("customer-1")["units"] != "196" {
		t.Fatalf("funding balance = %v", backend.GetBalances("customer-1"))
	}

	context := BillingContext{CustomerID: "customer-1", Service: "api_request", Variant: "default", ReferenceID: "request-2", Metadata: Metadata{}}
	context.Report(Zero(), MustAmount("1.5"), 2, 0, 0, 0, 0)
	if _, err := WriteUsageSession(context, false, true, backend); err != nil {
		t.Fatal(err)
	}
	worker := BillingWorker{Service: service, BatchSize: 10}
	cycle, err := worker.RunOnce()
	if err != nil || cycle.Processed != 1 {
		t.Fatalf("worker processed = %+v, %v", cycle, err)
	}

	failed, err := backend.Create(testUsageData("customer-1", 999))
	if err != nil {
		t.Fatal(err)
	}
	cycle, err = worker.RunOnce()
	if err != nil || cycle.Failed != 1 {
		t.Fatalf("worker failure = %+v, %v", cycle, err)
	}
	failedRecords := backend.GetByCustomer("customer-1", 0, 10)
	for _, record := range failedRecords {
		if record.ID == failed.ID && record.BillingStatus != StatusFailed {
			t.Fatalf("failed record status = %s", record.BillingStatus)
		}
	}

	if err := worker.Service.Charge("customer-1", "units", MustAmount("10"), ""); err != nil {
		t.Fatal(err)
	}
	if err := worker.Service.Refund("customer-1", "units", MustAmount("10"), ""); err != nil {
		t.Fatal(err)
	}
	snapshot := GetUsageSnapshot("customer-1", []string{"units"}, worker.Service.Cache, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if snapshot.Metrics["units"].Used != 16 || snapshot.Metrics["units"].Total != 210 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if backend.transactionCount() != 5 {
		t.Fatalf("transaction count = %d", backend.transactionCount())
	}

	backend.close()
	reopened := newSQLiteBackend(t, filename)
	defer reopened.close()
	if reopened.GetBalances("customer-1")["units"] != "194" || reopened.transactionCount() != 5 {
		t.Fatalf("reopened state = balances=%v transactions=%d", reopened.GetBalances("customer-1"), reopened.transactionCount())
	}
}
