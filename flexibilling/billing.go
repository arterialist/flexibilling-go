// Package flexibilling provides provider-agnostic usage metering and balance billing.
//
// Hosts implement BillingRepository and BillingCache for their own storage and
// transaction boundaries. The in-memory implementations are reference adapters
// for tests, examples, and local programs.
package flexibilling

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Amount is an exact decimal backed by a rational number. Values are serialized
// as canonical decimal strings and never use binary floating point for billing.
type Amount struct{ rat *big.Rat }

func ParseAmount(value string) (Amount, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Amount{}, fmt.Errorf("empty amount")
	}
	sign := 1
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		parsed, err := strconv.Atoi(value[index+1:])
		if err != nil {
			return Amount{}, fmt.Errorf("invalid amount exponent: %w", err)
		}
		exponent = parsed
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || (len(parts) == 2 && parts[0] == "" && parts[1] == "") {
		return Amount{}, fmt.Errorf("invalid decimal amount %q", value)
	}
	whole := parts[0]
	if whole == "" {
		whole = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	digits := whole + fraction
	if digits == "" {
		digits = "0"
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return Amount{}, fmt.Errorf("invalid decimal amount %q", value)
		}
	}
	numerator := new(big.Int)
	if _, ok := numerator.SetString(digits, 10); !ok {
		return Amount{}, fmt.Errorf("invalid decimal amount %q", value)
	}
	if sign < 0 {
		numerator.Neg(numerator)
	}
	scale := len(fraction) - exponent
	denominator := big.NewInt(1)
	if scale > 0 {
		denominator.Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	} else if scale < 0 {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scale)), nil)
		numerator.Mul(numerator, factor)
	}
	return Amount{rat: new(big.Rat).SetFrac(numerator, denominator)}, nil
}

func MustAmount(value string) Amount {
	amount, err := ParseAmount(value)
	if err != nil {
		panic(err)
	}
	return amount
}

func AmountFromInt(value int64) Amount { return Amount{rat: new(big.Rat).SetInt64(value)} }

func Zero() Amount { return AmountFromInt(0) }

func (amount Amount) normalized() *big.Rat {
	if amount.rat == nil {
		return new(big.Rat)
	}
	return amount.rat
}

func (amount Amount) Add(other Amount) Amount {
	return Amount{rat: new(big.Rat).Add(amount.normalized(), other.normalized())}
}
func (amount Amount) Sub(other Amount) Amount {
	return Amount{rat: new(big.Rat).Sub(amount.normalized(), other.normalized())}
}
func (amount Amount) Mul(other Amount) Amount {
	return Amount{rat: new(big.Rat).Mul(amount.normalized(), other.normalized())}
}
func (amount Amount) Neg() Amount               { return Amount{rat: new(big.Rat).Neg(amount.normalized())} }
func (amount Amount) Abs() Amount               { return Amount{rat: new(big.Rat).Abs(amount.normalized())} }
func (amount Amount) Cmp(other Amount) int      { return amount.normalized().Cmp(other.normalized()) }
func (amount Amount) GreaterThanZero() bool     { return amount.Cmp(Zero()) > 0 }
func (amount Amount) LessThanOrEqualZero() bool { return amount.Cmp(Zero()) <= 0 }
func (amount Amount) IsZero() bool              { return amount.Cmp(Zero()) == 0 }
func (amount Amount) Float64() float64 {
	value, _ := strconv.ParseFloat(amount.String(), 64)
	return value
}

func (amount Amount) String() string {
	rat := amount.normalized()
	numerator := new(big.Int).Set(rat.Num())
	denominator := new(big.Int).Set(rat.Denom())
	negative := numerator.Sign() < 0
	numerator.Abs(numerator)
	countTwo, countFive := factorCount(denominator, 2), factorCount(denominator, 5)
	scale := countTwo
	if countFive > scale {
		scale = countFive
	}
	if scale > 0 {
		if countTwo < scale {
			numerator.Mul(numerator, new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(scale-countTwo)), nil))
		}
		if countFive < scale {
			numerator.Mul(numerator, new(big.Int).Exp(big.NewInt(5), big.NewInt(int64(scale-countFive)), nil))
		}
	}
	base := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(numerator, base, fraction)
	result := whole.String()
	if scale > 0 && fraction.Sign() != 0 {
		fractionText := fraction.String()
		if padding := scale - len(fractionText); padding > 0 {
			fractionText = strings.Repeat("0", padding) + fractionText
		}
		fractionText = strings.TrimRight(fractionText, "0")
		if fractionText != "" {
			result += "." + fractionText
		}
	}
	if negative && result != "0" {
		return "-" + result
	}
	return result
}

func factorCount(value *big.Int, factor int64) int {
	remaining := new(big.Int).Set(value)
	divisor := big.NewInt(factor)
	count := 0
	for {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(remaining, divisor, remainder)
		if remainder.Sign() != 0 {
			return count
		}
		count++
		remaining = quotient
	}
}

func (amount Amount) MarshalJSON() ([]byte, error) { return json.Marshal(amount.String()) }

func (amount *Amount) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseAmount(value)
	if err != nil {
		return err
	}
	*amount = parsed
	return nil
}

type Metadata map[string]any
type CustomerId = string
type RecordId = string
type AssetName = string
type ServiceName = string

const (
	MetricFixed    = "fixed"
	MetricQuantity = "quantity"
	MetricDuration = "duration"
	MetricUnits    = "units"
)

const (
	TransactionUsage        = "usage"
	TransactionTopUp        = "top_up"
	TransactionMonthlyGrant = "monthly_grant"
	TransactionExpiration   = "expiration"
	TransactionRefund       = "refund"
)

const (
	ProductTopUp        = "top_up"
	ProductMonthlyQuota = "monthly_quota"
)

const (
	StatusPending   = "pending"
	StatusProcessed = "processed"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

type CustomerBalance struct {
	CustomerID CustomerId
	AssetType  AssetName
	Amount     Amount
	ID         RecordId
}

type BalanceTransaction struct {
	CustomerID       CustomerId
	AssetType        AssetName
	Amount           Amount
	BalanceAfter     Amount
	TransactionType  string
	ID               RecordId
	SourceUsageID    RecordId
	PaymentReference string
	Description      string
	CreatedAt        time.Time
}

type BalanceTransactionCreate struct {
	CustomerID       CustomerId
	AssetType        AssetName
	Amount           Amount
	BalanceAfter     Amount
	TransactionType  string
	SourceUsageID    RecordId
	PaymentReference string
	Description      string
}

type BillingRule struct {
	Service           ServiceName
	TargetAsset       AssetName
	MetricType        string
	ConversionRate    Amount
	Priority          int
	FilterCondition   Metadata
	RefundServiceType ServiceName
	IsActive          bool
	ID                RecordId
}

type BillingProduct struct {
	ExternalProductID string
	AssetType         AssetName
	Amount            Amount
	Strategy          string
	Description       string
	IsActive          bool
	ID                RecordId
}

type UsageRecord struct {
	CustomerID          CustomerId
	Service             ServiceName
	Variant             string
	ID                  RecordId
	ReferenceID         RecordId
	Quantity            *Amount
	DurationSeconds     *Amount
	Units               *int64
	InputUnits          *int64
	OutputUnits         *int64
	CachedUnits         *int64
	BillingStatus       string
	BillingErrorMessage string
	EventMetadata       Metadata
	CreatedAt           time.Time
}

func NewUsageRecord(customerID, service string) UsageRecord {
	return UsageRecord{CustomerID: customerID, Service: service, Variant: "default", BillingStatus: StatusPending}
}

type UsageRecordCreate struct {
	CustomerID          CustomerId
	Service             ServiceName
	Variant             string
	ReferenceID         RecordId
	Quantity            *Amount
	DurationSeconds     *Amount
	Units               *int64
	InputUnits          *int64
	OutputUnits         *int64
	CachedUnits         *int64
	BillingStatus       string
	BillingErrorMessage string
	EventMetadata       Metadata
}

type UsageSummary struct {
	Service              ServiceName
	Variant              string
	UsageCount           int64
	TotalQuantity        *Amount
	TotalDurationSeconds *Amount
	TotalUnits           *int64
	TotalInputUnits      *int64
	TotalOutputUnits     *int64
	TotalCachedUnits     *int64
}

type BillingStats struct {
	UsageCount int64
	Quantity   Amount
	Spend      Amount
	Custom     map[string]Amount
}

type ActivityEvent struct {
	Time   string
	Action string
	Cost   string
	Result string
}

const (
	ErrorInsufficientFunds = "insufficient_funds"
	ErrorNoBillableUsage   = "no_billable_usage"
	ErrorRuleNotFound      = "rule_not_found"
	ErrorGatekeeperDenied  = "gatekeeper_denied"
	ErrorConfiguration     = "configuration"
	ErrorAdapter           = "adapter"
	ErrorInvalidAmount     = "invalid_amount"
)

type BillingError struct {
	Kind       string
	CustomerID string
	Service    string
	Message    string
}

func (err *BillingError) Error() string { return err.Message }

func newBillingError(kind, message string) *BillingError {
	return &BillingError{Kind: kind, Message: message}
}

type BillingRepository interface {
	GetActiveRules(service string) []BillingRule
	GetCustomerBalances(customerID string) []CustomerBalance
	UpsertBalance(customerID, assetType string, amount Amount) (CustomerBalance, error)
	DecrementBalance(customerID, assetType string, deduction Amount) (Amount, error)
	IncrementBalance(customerID, assetType string, addition Amount) (Amount, error)
	CreateTransaction(data BalanceTransactionCreate) (BalanceTransaction, error)
	GetTransactionForUsage(referenceID, service, customerID string) (BalanceTransaction, bool)
	GetTransactionByReference(paymentReference string) (BalanceTransaction, bool)
	GetProductsForExternalIDs(productIDs []string) []BillingProduct
	GetPendingRecords(limit int) []UsageRecord
	MarkRecordProcessed(recordID string) error
	MarkRecordFailed(recordID, message string) error
	MarkRecordSkipped(recordID string) error
}

type UsageRepository interface {
	Create(data UsageRecordCreate) (*UsageRecord, error)
	GetByCustomer(customerID string, skip, limit int) []UsageRecord
	GetUsageSummary(customerID string, from, to *time.Time) []UsageSummary
	GetUsageRecords(customerID string, from, to *time.Time, service string, limit, offset int) ([]UsageRecord, int)
}

type BillingCache interface {
	SetBalances(customerID string, balances map[string]Amount) error
	UpdateSingleBalance(customerID, assetType string, amount Amount) error
	GetBalances(customerID string) map[string]string
	CanTransact(customerID string) bool
	GetAssetAmount(customerID, assetType string) (Amount, bool)
	DeleteBalances(customerID string) error
	IncrementStats(customerID, month string, stats BillingStats) error
	GetStats(customerID, month string) map[string]string
	PushFeedEvent(customerID string, event ActivityEvent) error
	GetFeed(customerID string, limit int) []ActivityEvent
	DeleteCustomerCache(customerID string) error
}

func Rate(rule BillingRule, record UsageRecord) (Amount, error) {
	switch rule.MetricType {
	case MetricFixed:
		return rule.ConversionRate, nil
	case MetricQuantity:
		if record.Quantity == nil {
			return Zero(), nil
		}
		return record.Quantity.Mul(rule.ConversionRate), nil
	case MetricDuration:
		return extractDuration(record).Mul(rule.ConversionRate), nil
	case MetricUnits:
		return AmountFromInt(extractUnits(record)).Mul(rule.ConversionRate), nil
	default:
		return Zero(), newBillingError(ErrorConfiguration, fmt.Sprintf("unknown metric type: %s", rule.MetricType))
	}
}

func MatchesFilter(rule BillingRule, metadata Metadata) bool {
	if len(rule.FilterCondition) == 0 {
		return true
	}
	if metadata == nil {
		return false
	}
	for key, expected := range rule.FilterCondition {
		actual, ok := resolveDottedKey(metadata, key)
		if !ok || fmt.Sprint(actual) != fmt.Sprint(expected) {
			return false
		}
	}
	return true
}

func resolveDottedKey(metadata Metadata, key string) (any, bool) {
	var current any = metadata
	for _, part := range strings.Split(key, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func extractDuration(record UsageRecord) Amount {
	if value, ok := record.EventMetadata["duration_seconds"]; ok {
		switch typed := value.(type) {
		case string:
			if amount, err := ParseAmount(typed); err == nil {
				return amount
			}
		case float64:
			return MustAmount(strconv.FormatFloat(typed, 'f', -1, 64))
		}
	}
	if record.DurationSeconds != nil {
		return *record.DurationSeconds
	}
	return Zero()
}

func extractUnits(record UsageRecord) int64 {
	if record.Units != nil {
		return *record.Units
	}
	var units int64
	if record.InputUnits != nil {
		units += *record.InputUnits
	}
	if record.OutputUnits != nil {
		units += *record.OutputUnits
	}
	return units
}

type WaterfallResult struct {
	AssetType         AssetName
	Amount            Amount
	Rule              BillingRule
	RefundServiceType ServiceName
}

func EvaluateWaterfall(rules []BillingRule, record UsageRecord, balances map[string]Amount) (WaterfallResult, error) {
	if len(rules) == 0 {
		return WaterfallResult{}, &BillingError{Kind: ErrorRuleNotFound, Service: record.Service, Message: fmt.Sprintf("No active billing rules found for service '%s'", record.Service)}
	}
	ordered := append([]BillingRule(nil), rules...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Priority < ordered[right].Priority })
	sawPositive := false
	for _, rule := range ordered {
		if !rule.IsActive || !MatchesFilter(rule, record.EventMetadata) {
			continue
		}
		cost, err := Rate(rule, record)
		if err != nil {
			return WaterfallResult{}, err
		}
		if cost.LessThanOrEqualZero() {
			continue
		}
		sawPositive = true
		if balances[rule.TargetAsset].Cmp(cost) >= 0 {
			return WaterfallResult{AssetType: rule.TargetAsset, Amount: cost, Rule: rule, RefundServiceType: rule.RefundServiceType}, nil
		}
	}
	if !sawPositive {
		return WaterfallResult{}, &BillingError{Kind: ErrorNoBillableUsage, CustomerID: record.CustomerID, Service: record.Service, Message: fmt.Sprintf("No billable usage for customer %s service '%s'", record.CustomerID, record.Service)}
	}
	return WaterfallResult{}, &BillingError{Kind: ErrorInsufficientFunds, CustomerID: record.CustomerID, Service: record.Service, Message: fmt.Sprintf("Customer %s has insufficient funds for service '%s'", record.CustomerID, record.Service)}
}

type InMemoryBillingRepository struct {
	Rules        []BillingRule
	Products     []BillingProduct
	Records      []UsageRecord
	Transactions []BalanceTransaction
	Balances     map[string]Amount
	nextID       int
}

func NewInMemoryBillingRepository() *InMemoryBillingRepository {
	return &InMemoryBillingRepository{Balances: map[string]Amount{}, nextID: 1}
}
func balanceKey(customerID, assetType string) string { return customerID + ":" + assetType }

func (repo *InMemoryBillingRepository) GetActiveRules(service string) []BillingRule {
	result := make([]BillingRule, 0)
	for _, rule := range repo.Rules {
		if rule.IsActive && rule.Service == service {
			result = append(result, rule)
		}
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].Priority < result[right].Priority })
	return result
}

func (repo *InMemoryBillingRepository) GetCustomerBalances(customerID string) []CustomerBalance {
	result := make([]CustomerBalance, 0)
	prefix := customerID + ":"
	for key, amount := range repo.Balances {
		if strings.HasPrefix(key, prefix) {
			result = append(result, CustomerBalance{CustomerID: customerID, AssetType: strings.TrimPrefix(key, prefix), Amount: amount})
		}
	}
	return result
}

func (repo *InMemoryBillingRepository) UpsertBalance(customerID, assetType string, amount Amount) (CustomerBalance, error) {
	if repo.Balances == nil {
		repo.Balances = map[string]Amount{}
	}
	repo.Balances[balanceKey(customerID, assetType)] = amount
	return CustomerBalance{CustomerID: customerID, AssetType: assetType, Amount: amount}, nil
}

func (repo *InMemoryBillingRepository) DecrementBalance(customerID, assetType string, deduction Amount) (Amount, error) {
	key := balanceKey(customerID, assetType)
	current := repo.Balances[key]
	if current.Cmp(deduction) < 0 {
		return Zero(), &BillingError{Kind: ErrorInsufficientFunds, CustomerID: customerID, Service: "charge", Message: fmt.Sprintf("Customer %s has insufficient funds for service 'charge'", customerID)}
	}
	next := current.Sub(deduction)
	repo.Balances[key] = next
	return next, nil
}

func (repo *InMemoryBillingRepository) IncrementBalance(customerID, assetType string, addition Amount) (Amount, error) {
	key := balanceKey(customerID, assetType)
	next := repo.Balances[key].Add(addition)
	repo.Balances[key] = next
	return next, nil
}

func (repo *InMemoryBillingRepository) CreateTransaction(data BalanceTransactionCreate) (BalanceTransaction, error) {
	transaction := BalanceTransaction{CustomerID: data.CustomerID, AssetType: data.AssetType, Amount: data.Amount, BalanceAfter: data.BalanceAfter, TransactionType: data.TransactionType, ID: fmt.Sprint(repo.nextID), SourceUsageID: data.SourceUsageID, PaymentReference: data.PaymentReference, Description: data.Description, CreatedAt: time.Now().UTC()}
	repo.nextID++
	repo.Transactions = append(repo.Transactions, transaction)
	return transaction, nil
}

func (repo *InMemoryBillingRepository) GetTransactionForUsage(referenceID, service, customerID string) (BalanceTransaction, bool) {
	ids := map[string]bool{}
	for _, record := range repo.Records {
		if record.ReferenceID == referenceID && record.Service == service {
			ids[record.ID] = true
		}
	}
	for index := len(repo.Transactions) - 1; index >= 0; index-- {
		transaction := repo.Transactions[index]
		if transaction.CustomerID == customerID && ids[transaction.SourceUsageID] && transaction.TransactionType == TransactionUsage {
			return transaction, true
		}
	}
	return BalanceTransaction{}, false
}

func (repo *InMemoryBillingRepository) GetTransactionByReference(paymentReference string) (BalanceTransaction, bool) {
	for _, transaction := range repo.Transactions {
		if transaction.PaymentReference == paymentReference {
			return transaction, true
		}
	}
	return BalanceTransaction{}, false
}
func (repo *InMemoryBillingRepository) GetProductsForExternalIDs(productIDs []string) []BillingProduct {
	wanted := map[string]bool{}
	for _, id := range productIDs {
		wanted[id] = true
	}
	result := []BillingProduct{}
	for _, product := range repo.Products {
		if product.IsActive && wanted[product.ExternalProductID] {
			result = append(result, product)
		}
	}
	return result
}
func (repo *InMemoryBillingRepository) GetPendingRecords(limit int) []UsageRecord {
	result := []UsageRecord{}
	for _, record := range repo.Records {
		if record.BillingStatus == StatusPending {
			result = append(result, record)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func (repo *InMemoryBillingRepository) recordIndex(recordID string) (int, error) {
	for index := range repo.Records {
		if repo.Records[index].ID == recordID {
			return index, nil
		}
	}
	return -1, newBillingError(ErrorAdapter, "unknown usage record: "+recordID)
}
func (repo *InMemoryBillingRepository) MarkRecordProcessed(recordID string) error {
	index, err := repo.recordIndex(recordID)
	if err != nil {
		return err
	}
	repo.Records[index].BillingStatus = StatusProcessed
	return nil
}
func (repo *InMemoryBillingRepository) MarkRecordFailed(recordID, message string) error {
	index, err := repo.recordIndex(recordID)
	if err != nil {
		return err
	}
	repo.Records[index].BillingStatus = StatusFailed
	repo.Records[index].BillingErrorMessage = message
	return nil
}
func (repo *InMemoryBillingRepository) MarkRecordSkipped(recordID string) error {
	index, err := repo.recordIndex(recordID)
	if err != nil {
		return err
	}
	repo.Records[index].BillingStatus = StatusSkipped
	return nil
}

func (repo *InMemoryBillingRepository) Create(data UsageRecordCreate) (*UsageRecord, error) {
	status := data.BillingStatus
	if status == "" {
		status = StatusPending
	}
	record := UsageRecord{CustomerID: data.CustomerID, Service: data.Service, Variant: data.Variant, ID: fmt.Sprintf("usage-%d", len(repo.Records)+1), ReferenceID: data.ReferenceID, Quantity: data.Quantity, DurationSeconds: data.DurationSeconds, Units: data.Units, InputUnits: data.InputUnits, OutputUnits: data.OutputUnits, CachedUnits: data.CachedUnits, BillingStatus: status, BillingErrorMessage: data.BillingErrorMessage, EventMetadata: data.EventMetadata, CreatedAt: time.Now().UTC()}
	repo.Records = append(repo.Records, record)
	return &repo.Records[len(repo.Records)-1], nil
}

func (repo *InMemoryBillingRepository) GetByCustomer(customerID string, skip, limit int) []UsageRecord {
	records := repo.filteredWithDates(customerID, nil, nil, "")
	sort.SliceStable(records, func(left, right int) bool { return records[left].CreatedAt.After(records[right].CreatedAt) })
	if skip > len(records) {
		return []UsageRecord{}
	}
	end := skip + limit
	if end > len(records) {
		end = len(records)
	}
	return records[skip:end]
}
func (repo *InMemoryBillingRepository) GetUsageSummary(customerID string, from, to *time.Time) []UsageSummary {
	records := repo.filteredWithDates(customerID, from, to, "")
	groups := map[string][]UsageRecord{}
	for _, record := range records {
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
func (repo *InMemoryBillingRepository) GetUsageRecords(customerID string, from, to *time.Time, service string, limit, offset int) ([]UsageRecord, int) {
	records := repo.filteredWithDates(customerID, from, to, service)
	sort.SliceStable(records, func(left, right int) bool { return records[left].CreatedAt.After(records[right].CreatedAt) })
	total := len(records)
	if offset > total {
		return []UsageRecord{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return records[offset:end], total
}
func (repo *InMemoryBillingRepository) filteredWithDates(customerID string, from, to *time.Time, service string) []UsageRecord {
	result := []UsageRecord{}
	for _, record := range repo.Records {
		if record.CustomerID != customerID || (service != "" && record.Service != service) {
			continue
		}
		if from != nil && (record.CreatedAt.IsZero() || record.CreatedAt.Before(*from)) {
			continue
		}
		if to != nil && (record.CreatedAt.IsZero() || record.CreatedAt.After(*to)) {
			continue
		}
		result = append(result, record)
	}
	return result
}
func sumAmounts(records []UsageRecord, getter func(UsageRecord) *Amount) *Amount {
	var total Amount
	found := false
	for _, record := range records {
		if value := getter(record); value != nil {
			total = total.Add(*value)
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}
func sumInts(records []UsageRecord, getter func(UsageRecord) *int64) *int64 {
	var total int64
	found := false
	for _, record := range records {
		if value := getter(record); value != nil {
			total += *value
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

type InMemoryBillingCache struct {
	balances map[string]map[string]Amount
	stats    map[string]map[string]Amount
	feed     map[string][]ActivityEvent
}

func NewInMemoryBillingCache() *InMemoryBillingCache {
	return &InMemoryBillingCache{balances: map[string]map[string]Amount{}, stats: map[string]map[string]Amount{}, feed: map[string][]ActivityEvent{}}
}
func (cache *InMemoryBillingCache) SetBalances(customerID string, values map[string]Amount) error {
	copyValues := map[string]Amount{}
	for key, value := range values {
		copyValues[key] = value
	}
	cache.balances[customerID] = copyValues
	return nil
}
func (cache *InMemoryBillingCache) UpdateSingleBalance(customerID, assetType string, amount Amount) error {
	if cache.balances[customerID] == nil {
		cache.balances[customerID] = map[string]Amount{}
	}
	cache.balances[customerID][assetType] = amount
	return nil
}
func (cache *InMemoryBillingCache) GetBalances(customerID string) map[string]string {
	result := map[string]string{}
	for key, value := range cache.balances[customerID] {
		result[key] = value.String()
	}
	return result
}
func (cache *InMemoryBillingCache) CanTransact(customerID string) bool {
	for _, value := range cache.balances[customerID] {
		if value.GreaterThanZero() {
			return true
		}
	}
	return false
}
func (cache *InMemoryBillingCache) GetAssetAmount(customerID, assetType string) (Amount, bool) {
	value, ok := cache.balances[customerID][assetType]
	return value, ok
}
func (cache *InMemoryBillingCache) DeleteBalances(customerID string) error {
	delete(cache.balances, customerID)
	return nil
}
func (cache *InMemoryBillingCache) IncrementStats(customerID, month string, stats BillingStats) error {
	key := customerID + ":" + month
	if cache.stats[key] == nil {
		cache.stats[key] = map[string]Amount{}
	}
	current := cache.stats[key]
	if stats.UsageCount != 0 {
		current["total_usage_count"] = current["total_usage_count"].Add(AmountFromInt(stats.UsageCount))
	}
	if !stats.Quantity.IsZero() {
		current["total_quantity"] = current["total_quantity"].Add(stats.Quantity)
	}
	if !stats.Spend.IsZero() {
		current["total_spend"] = current["total_spend"].Add(stats.Spend)
	}
	for name, value := range stats.Custom {
		current["total_custom:"+name] = current["total_custom:"+name].Add(value)
	}
	return nil
}
func (cache *InMemoryBillingCache) GetStats(customerID, month string) map[string]string {
	result := map[string]string{}
	for key, value := range cache.stats[customerID+":"+month] {
		result[key] = value.String()
	}
	return result
}
func (cache *InMemoryBillingCache) PushFeedEvent(customerID string, event ActivityEvent) error {
	cache.feed[customerID] = append([]ActivityEvent{event}, cache.feed[customerID]...)
	if len(cache.feed[customerID]) > 50 {
		cache.feed[customerID] = cache.feed[customerID][:50]
	}
	return nil
}
func (cache *InMemoryBillingCache) GetFeed(customerID string, limit int) []ActivityEvent {
	events := cache.feed[customerID]
	if limit > len(events) {
		limit = len(events)
	}
	return append([]ActivityEvent(nil), events[:limit]...)
}
func (cache *InMemoryBillingCache) DeleteCustomerCache(customerID string) error {
	delete(cache.balances, customerID)
	delete(cache.feed, customerID)
	for key := range cache.stats {
		if strings.HasPrefix(key, customerID+":") {
			delete(cache.stats, key)
		}
	}
	return nil
}

type NullBillingCache struct{}

func (NullBillingCache) SetBalances(string, map[string]Amount) error       { return nil }
func (NullBillingCache) UpdateSingleBalance(string, string, Amount) error  { return nil }
func (NullBillingCache) GetBalances(string) map[string]string              { return map[string]string{} }
func (NullBillingCache) CanTransact(string) bool                           { return false }
func (NullBillingCache) GetAssetAmount(string, string) (Amount, bool)      { return Zero(), false }
func (NullBillingCache) DeleteBalances(string) error                       { return nil }
func (NullBillingCache) IncrementStats(string, string, BillingStats) error { return nil }
func (NullBillingCache) GetStats(string, string) map[string]string         { return map[string]string{} }
func (NullBillingCache) PushFeedEvent(string, ActivityEvent) error         { return nil }
func (NullBillingCache) GetFeed(string, int) []ActivityEvent               { return []ActivityEvent{} }
func (NullBillingCache) DeleteCustomerCache(string) error                  { return nil }

type BillingService struct {
	Repo  BillingRepository
	Cache BillingCache
	Clock func() time.Time
}

func NewBillingService(repo BillingRepository, cache BillingCache) *BillingService {
	return &BillingService{Repo: repo, Cache: cache, Clock: func() time.Time { return time.Now().UTC() }}
}
func (service *BillingService) month() string { return service.Clock().UTC().Format("2006-01") }

func (service *BillingService) ProcessRecord(record *UsageRecord) error {
	rules := service.Repo.GetActiveRules(record.Service)
	rows := service.Repo.GetCustomerBalances(record.CustomerID)
	balances := map[string]Amount{}
	for _, row := range rows {
		balances[row.AssetType] = row.Amount
	}
	result, err := EvaluateWaterfall(rules, *record, balances)
	if err != nil {
		return err
	}
	newAmount, err := service.Repo.DecrementBalance(record.CustomerID, result.AssetType, result.Amount)
	if err != nil {
		return err
	}
	_, err = service.Repo.CreateTransaction(BalanceTransactionCreate{CustomerID: record.CustomerID, AssetType: result.AssetType, Amount: result.Amount.Neg(), BalanceAfter: newAmount, TransactionType: TransactionUsage, SourceUsageID: record.ID, Description: fmt.Sprintf("%s usage: -%s %s", record.Service, result.Amount.String(), result.AssetType)})
	if err != nil {
		return err
	}
	if result.RefundServiceType != "" && record.ReferenceID != "" {
		if err := service.handleRefund(*record, result.RefundServiceType); err != nil {
			return err
		}
	}
	if record.ID == "" {
		return newBillingError(ErrorConfiguration, "a usage record must have an id before it can be processed")
	}
	if err := service.Repo.MarkRecordProcessed(record.ID); err != nil {
		return err
	}
	record.BillingStatus = StatusProcessed
	return service.syncCache(*record, result, newAmount)
}

func (service *BillingService) CheckPermission(customerID string) error {
	if len(service.Cache.GetBalances(customerID)) == 0 || !service.Cache.CanTransact(customerID) {
		return &BillingError{Kind: ErrorGatekeeperDenied, CustomerID: customerID, Message: fmt.Sprintf("Gatekeeper denied: customer %s cannot transact", customerID)}
	}
	return nil
}
func (service *BillingService) CheckPermissionSilent(customerID string) bool {
	return service.CheckPermission(customerID) == nil
}
func (service *BillingService) RefreshCustomerBalanceCache(customerID string) error {
	rows := service.Repo.GetCustomerBalances(customerID)
	if len(rows) == 0 {
		return service.Cache.DeleteBalances(customerID)
	}
	values := map[string]Amount{}
	for _, row := range rows {
		values[row.AssetType] = row.Amount
	}
	return service.Cache.SetBalances(customerID, values)
}

func (service *BillingService) FundCustomer(customerID string, productIDs []string, paymentReference string) (bool, error) {
	if _, ok := service.Repo.GetTransactionByReference(paymentReference); ok {
		return false, nil
	}
	products := service.Repo.GetProductsForExternalIDs(productIDs)
	if len(products) == 0 {
		return false, nil
	}
	for _, product := range products {
		var newAmount Amount
		var transactionType string
		var description string
		switch product.Strategy {
		case ProductTopUp:
			var err error
			newAmount, err = service.Repo.IncrementBalance(customerID, product.AssetType, product.Amount)
			if err != nil {
				return false, err
			}
			transactionType = TransactionTopUp
			description = fmt.Sprintf("Top-up: +%s %s (product: %s)", product.Amount, product.AssetType, product.ExternalProductID)
		case ProductMonthlyQuota:
			balance, err := service.Repo.UpsertBalance(customerID, product.AssetType, product.Amount)
			if err != nil {
				return false, err
			}
			newAmount = balance.Amount
			transactionType = TransactionMonthlyGrant
			description = fmt.Sprintf("Monthly quota reset: %s %s (product: %s)", product.Amount, product.AssetType, product.ExternalProductID)
		default:
			return false, newBillingError(ErrorConfiguration, "unknown billing product strategy: "+product.Strategy)
		}
		if _, err := service.Repo.CreateTransaction(BalanceTransactionCreate{CustomerID: customerID, AssetType: product.AssetType, Amount: product.Amount, BalanceAfter: newAmount, TransactionType: transactionType, PaymentReference: paymentReference, Description: description}); err != nil {
			return false, err
		}
		if err := service.Cache.UpdateSingleBalance(customerID, product.AssetType, newAmount); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (service *BillingService) Charge(customerID, assetType string, amount Amount, description string) error {
	if amount.LessThanOrEqualZero() {
		return newBillingError(ErrorInvalidAmount, "charge amount must be positive")
	}
	newAmount, err := service.Repo.DecrementBalance(customerID, assetType, amount)
	if err != nil {
		return err
	}
	if _, err = service.Repo.CreateTransaction(BalanceTransactionCreate{CustomerID: customerID, AssetType: assetType, Amount: amount.Neg(), BalanceAfter: newAmount, TransactionType: TransactionUsage, Description: description}); err != nil {
		return err
	}
	if err = service.Cache.UpdateSingleBalance(customerID, assetType, newAmount); err != nil {
		return err
	}
	return service.Cache.IncrementStats(customerID, service.month(), BillingStats{UsageCount: 1, Quantity: amount, Spend: amount, Custom: map[string]Amount{"asset:" + assetType: amount}})
}
func (service *BillingService) Refund(customerID, assetType string, amount Amount, description string) error {
	if amount.LessThanOrEqualZero() {
		return newBillingError(ErrorInvalidAmount, "refund amount must be positive")
	}
	newAmount, err := service.Repo.IncrementBalance(customerID, assetType, amount)
	if err != nil {
		return err
	}
	if _, err = service.Repo.CreateTransaction(BalanceTransactionCreate{CustomerID: customerID, AssetType: assetType, Amount: amount, BalanceAfter: newAmount, TransactionType: TransactionRefund, Description: description}); err != nil {
		return err
	}
	return service.Cache.UpdateSingleBalance(customerID, assetType, newAmount)
}

func (service *BillingService) handleRefund(record UsageRecord, refundService string) error {
	original, ok := service.Repo.GetTransactionForUsage(record.ReferenceID, refundService, record.CustomerID)
	if !ok {
		return nil
	}
	amount := original.Amount.Abs()
	newAmount, err := service.Repo.IncrementBalance(record.CustomerID, original.AssetType, amount)
	if err != nil {
		return err
	}
	if _, err = service.Repo.CreateTransaction(BalanceTransactionCreate{CustomerID: record.CustomerID, AssetType: original.AssetType, Amount: amount, BalanceAfter: newAmount, TransactionType: TransactionRefund, SourceUsageID: record.ID, Description: fmt.Sprintf("Refund for reference %s: +%s %s", record.ReferenceID, amount, original.AssetType)}); err != nil {
		return err
	}
	return service.Cache.UpdateSingleBalance(record.CustomerID, original.AssetType, newAmount)
}
func (service *BillingService) syncCache(record UsageRecord, result WaterfallResult, newAmount Amount) error {
	if err := service.Cache.UpdateSingleBalance(record.CustomerID, result.AssetType, newAmount); err != nil {
		return err
	}
	amount := result.Amount
	if err := service.Cache.IncrementStats(record.CustomerID, service.month(), BillingStats{UsageCount: 1, Quantity: amount, Spend: amount, Custom: map[string]Amount{"asset:" + result.AssetType: amount}}); err != nil {
		return err
	}
	return service.Cache.PushFeedEvent(record.CustomerID, ActivityEvent{Time: time.Now().UTC().Format(time.RFC3339Nano), Action: result.Rule.Service, Cost: fmt.Sprintf("%s %s", amount, result.AssetType), Result: "Success"})
}

func HasBalance(customerID string, assets []string, cache BillingCache) bool {
	balances := cache.GetBalances(customerID)
	total := Zero()
	for _, asset := range assets {
		if value, err := ParseAmount(balances[asset]); err == nil {
			total = total.Add(value)
		}
	}
	return total.GreaterThanZero()
}
func RequireBalance(customerID string, assets []string, cache BillingCache) error {
	if HasBalance(customerID, assets, cache) {
		return nil
	}
	return &BillingError{Kind: ErrorGatekeeperDenied, CustomerID: customerID, Message: fmt.Sprintf("Gatekeeper denied: customer %s cannot transact", customerID)}
}

type UsageMetrics struct {
	Quantity                                            Amount
	DurationSeconds                                     Amount
	Units, InputUnits, OutputUnits, CachedUnits, Events int64
}

func (metrics UsageMetrics) Empty() bool {
	return metrics.Quantity.IsZero() && metrics.DurationSeconds.IsZero() && metrics.Units == 0 && metrics.InputUnits == 0 && metrics.OutputUnits == 0 && metrics.CachedUnits == 0 && metrics.Events == 0
}

type BillingContext struct {
	CustomerID  string
	Service     string
	Variant     string
	ReferenceID string
	Metadata    Metadata
	Metrics     UsageMetrics
}

func (context *BillingContext) Report(quantity, duration Amount, units, inputUnits, outputUnits, cachedUnits, events int64) {
	context.Metrics.Quantity = context.Metrics.Quantity.Add(quantity)
	context.Metrics.DurationSeconds = context.Metrics.DurationSeconds.Add(duration)
	context.Metrics.Units += units
	context.Metrics.InputUnits += inputUnits
	context.Metrics.OutputUnits += outputUnits
	context.Metrics.CachedUnits += cachedUnits
	context.Metrics.Events += events
}
func WriteUsageSession(context BillingContext, failed, writeOnException bool, repository UsageRepository) (*UsageRecord, error) {
	if context.Metrics.Empty() || (failed && !writeOnException) {
		return nil, nil
	}
	metadata := map[string]any{}
	for key, value := range context.Metadata {
		metadata[key] = value
	}
	if !context.Metrics.DurationSeconds.IsZero() {
		metadata["duration_seconds"] = context.Metrics.DurationSeconds.Float64()
	}
	var quantity, duration *Amount
	if !context.Metrics.Quantity.IsZero() {
		value := context.Metrics.Quantity
		quantity = &value
	}
	if !context.Metrics.DurationSeconds.IsZero() {
		value := context.Metrics.DurationSeconds
		duration = &value
	}
	data := UsageRecordCreate{CustomerID: context.CustomerID, Service: context.Service, Variant: context.Variant, ReferenceID: context.ReferenceID, Quantity: quantity, DurationSeconds: duration, BillingStatus: StatusPending, EventMetadata: metadata}
	if context.Metrics.Units != 0 {
		value := context.Metrics.Units
		data.Units = &value
	}
	if context.Metrics.InputUnits != 0 {
		value := context.Metrics.InputUnits
		data.InputUnits = &value
	}
	if context.Metrics.OutputUnits != 0 {
		value := context.Metrics.OutputUnits
		data.OutputUnits = &value
	}
	if context.Metrics.CachedUnits != 0 {
		value := context.Metrics.CachedUnits
		data.CachedUnits = &value
	}
	return repository.Create(data)
}

type WorkerCycleResult struct{ Fetched, Processed, Skipped, Failed, Retried int }
type BillingWorker struct {
	Service   *BillingService
	BatchSize int
}

func (worker *BillingWorker) RunOnce() (WorkerCycleResult, error) {
	records := worker.Service.Repo.GetPendingRecords(worker.BatchSize)
	result := WorkerCycleResult{Fetched: len(records)}
	for _, record := range records {
		err := worker.Service.ProcessRecord(&record)
		if err == nil {
			result.Processed++
			continue
		}
		var billingErr *BillingError
		if errors.As(err, &billingErr) && billingErr.Kind == ErrorNoBillableUsage {
			result.Skipped++
			if record.ID != "" {
				if markErr := worker.Service.Repo.MarkRecordSkipped(record.ID); markErr != nil {
					return result, markErr
				}
			}
			continue
		}
		if errors.As(err, &billingErr) && (billingErr.Kind == ErrorInsufficientFunds || billingErr.Kind == ErrorRuleNotFound || billingErr.Kind == ErrorConfiguration) {
			result.Failed++
			if record.ID != "" {
				if markErr := worker.Service.Repo.MarkRecordFailed(record.ID, err.Error()); markErr != nil {
					return result, markErr
				}
			}
			continue
		}
		result.Retried++
	}
	return result, nil
}

type UsageMetric struct{ Used, Total float64 }
type UsageSnapshot struct {
	Period  string
	Metrics map[string]UsageMetric
}

func GetUsageSnapshot(customerID string, assets []string, cache BillingCache, now time.Time) UsageSnapshot {
	period := now.UTC().Format("2006-01")
	balances := cache.GetBalances(customerID)
	stats := cache.GetStats(customerID, period)
	metrics := map[string]UsageMetric{}
	for _, asset := range assets {
		used := Zero()
		if value, err := ParseAmount(stats["total_custom:asset:"+asset]); err == nil {
			used = value
		}
		remaining := Zero()
		if value, err := ParseAmount(balances[asset]); err == nil && value.GreaterThanZero() {
			remaining = value
		}
		metrics[asset] = UsageMetric{Used: used.Float64(), Total: used.Add(remaining).Float64()}
	}
	return UsageSnapshot{Period: period, Metrics: metrics}
}
