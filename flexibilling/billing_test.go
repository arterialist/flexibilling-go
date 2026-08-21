package flexibilling

import (
	"errors"
	"testing"
	"time"
)

func int64Ptr(value int64) *int64    { return &value }
func amountPtr(value string) *Amount { parsed := MustAmount(value); return &parsed }

func testRule(metric, asset string, priority int) BillingRule {
	return BillingRule{
		Service:        "api_request",
		TargetAsset:    asset,
		MetricType:     metric,
		ConversionRate: MustAmount("1"),
		Priority:       priority,
		IsActive:       true,
	}
}

func TestRatingAndWaterfall(t *testing.T) {
	record := NewUsageRecord("customer-1", "api_request")
	record.Quantity = amountPtr("3")
	record.DurationSeconds = amountPtr("60")
	record.InputUnits = int64Ptr(500)
	record.OutputUnits = int64Ptr(200)

	quantityRule := testRule(MetricQuantity, "units", 1)
	quantityRule.ConversionRate = MustAmount("2")
	cost, err := Rate(quantityRule, record)
	if err != nil || cost.String() != "6" {
		t.Fatalf("quantity rating = %s, %v", cost, err)
	}
	unitsRule := testRule(MetricUnits, "units", 1)
	unitsRule.ConversionRate = MustAmount("0.001")
	cost, err = Rate(unitsRule, record)
	if err != nil || cost.String() != "0.7" {
		t.Fatalf("units rating = %s, %v", cost, err)
	}

	record.Units = int64Ptr(60)
	result, err := EvaluateWaterfall(
		[]BillingRule{testRule(MetricUnits, "units", 10), testRule(MetricUnits, "prepaid_units", 20)},
		record,
		map[string]Amount{"units": Zero(), "prepaid_units": MustAmount("200")},
	)
	if err != nil || result.AssetType != "prepaid_units" || result.Amount.String() != "60" {
		t.Fatalf("waterfall result = %+v, %v", result, err)
	}

	record.Units = nil
	record.InputUnits = nil
	record.OutputUnits = nil
	_, err = EvaluateWaterfall([]BillingRule{testRule(MetricUnits, "units", 1)}, record, map[string]Amount{"units": MustAmount("100")})
	var billingErr *BillingError
	if !errors.As(err, &billingErr) || billingErr.Kind != ErrorNoBillableUsage {
		t.Fatalf("expected no-billable-usage, got %v", err)
	}
}

func TestServiceFundingSessionsAndSnapshot(t *testing.T) {
	repository := NewInMemoryBillingRepository()
	repository.Rules = append(repository.Rules, testRule(MetricUnits, "units", 1))
	repository.Products = append(repository.Products, BillingProduct{
		ExternalProductID: "plan-standard",
		AssetType:         "units",
		Amount:            MustAmount("100"),
		Strategy:          ProductTopUp,
		IsActive:          true,
	})
	if _, err := repository.UpsertBalance("customer-1", "units", MustAmount("100")); err != nil {
		t.Fatal(err)
	}
	service := NewBillingService(repository, NewInMemoryBillingCache())
	record := NewUsageRecord("customer-1", "api_request")
	record.ID = "usage-1"
	record.Units = int64Ptr(30)
	repository.Records = append(repository.Records, record)
	if err := service.ProcessRecord(&record); err != nil {
		t.Fatal(err)
	}
	if record.BillingStatus != StatusProcessed || service.Cache.GetBalances("customer-1")["units"] != "70" {
		t.Fatalf("record or balance not processed: %+v %v", record, service.Cache.GetBalances("customer-1"))
	}
	first, err := service.FundCustomer("customer-1", []string{"plan-standard"}, "payment-1")
	if err != nil || !first {
		t.Fatalf("first funding = %v, %v", first, err)
	}
	repeated, err := service.FundCustomer("customer-1", []string{"plan-standard"}, "payment-1")
	if err != nil || repeated {
		t.Fatalf("repeated funding = %v, %v", repeated, err)
	}
	if service.Repo.GetCustomerBalances("customer-1")[0].Amount.String() != "170" {
		t.Fatal("funding did not update the balance")
	}

	context := BillingContext{CustomerID: "customer-1", Service: "background_task", Variant: "default", Metadata: Metadata{}}
	context.Report(Zero(), MustAmount("95"), 0, 10, 0, 0, 0)
	if _, err := WriteUsageSession(context, false, true, service.Repo.(UsageRepository)); err != nil {
		t.Fatal(err)
	}
	if err := service.Charge("customer-1", "units", MustAmount("10"), ""); err != nil {
		t.Fatal(err)
	}
	if err := service.Refund("customer-1", "units", MustAmount("10"), ""); err != nil {
		t.Fatal(err)
	}
	snapshot := GetUsageSnapshot("customer-1", []string{"units"}, service.Cache, time.Now().UTC())
	if snapshot.Metrics["units"].Used != 40 || snapshot.Metrics["units"].Total != 210 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestWorkerMarksProcessedRecords(t *testing.T) {
	repository := NewInMemoryBillingRepository()
	repository.Rules = append(repository.Rules, testRule(MetricFixed, "units", 1))
	repository.UpsertBalance("customer-1", "units", MustAmount("2"))
	record := NewUsageRecord("customer-1", "api_request")
	record.ID = "usage-1"
	repository.Records = append(repository.Records, record)
	worker := BillingWorker{Service: NewBillingService(repository, NewInMemoryBillingCache()), BatchSize: 10}
	result, err := worker.RunOnce()
	if err != nil || result.Processed != 1 {
		t.Fatalf("worker = %+v, %v", result, err)
	}
}
