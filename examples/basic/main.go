package main

import (
	"fmt"

	billing "github.com/arterialist/flexibilling-go/flexibilling"
)

func main() {
	repository := billing.NewInMemoryBillingRepository()
	repository.Rules = append(repository.Rules, billing.BillingRule{
		Service:        "api_request",
		TargetAsset:    "units",
		MetricType:     billing.MetricUnits,
		ConversionRate: billing.MustAmount("1"),
		Priority:       10,
		IsActive:       true,
	})
	if _, err := repository.UpsertBalance("customer-1", "units", billing.MustAmount("100")); err != nil {
		panic(err)
	}
	record := billing.NewUsageRecord("customer-1", "api_request")
	record.ID = "usage-1"
	units := int64(12)
	record.Units = &units
	repository.Records = append(repository.Records, record)

	service := billing.NewBillingService(repository, billing.NewInMemoryBillingCache())
	if err := service.ProcessRecord(&record); err != nil {
		panic(err)
	}
	fmt.Println(record.BillingStatus)
}
