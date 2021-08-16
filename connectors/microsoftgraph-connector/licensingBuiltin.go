package main

import (
	"encoding/json"
	"fmt"

	"github.com/PaesslerAG/jsonpath"
	"github.com/gwos/tcg/transit"
)

const (
	licenseUri = "https://graph.microsoft.com/v1.0/subscribedSkus"
)

// AddonLicenseMetrics licensing built-in - could be data driven.
func AddonLicenseMetrics(service *transit.DynamicMonitoredService, token string) (err error) {
	var (
		c    int
		body []byte
		v    interface{}
	)

	if body, err = ExecuteRequest(licenseUri, token); err == nil {
		_ = json.Unmarshal(body, &v)
	} else {
		return
	}

	if c, err = getCount(v); err == nil {
		for i := 0; i < c; i++ {
			sku, _ := jsonpath.Get(fmt.Sprintf("$.value[%d].skuPartNumber", i), v)

			if definition, ok := contains(metricsProfile.Metrics, "licences.prepaid"); ok {
				prepaid, _ := jsonpath.Get(fmt.Sprintf("$.value[%d].prepaidUnits.enabled", i), v)
				metric := createMetricWithThresholds(
					sku.(string), ".licences.prepaid",
					prepaid.(float64),
					float64(definition.WarningThreshold),
					float64(definition.CriticalThreshold),
				)
				service.Metrics = append(service.Metrics, *metric)
			}

			if definition, ok := contains(metricsProfile.Metrics, "licences.consumed"); ok {
				consumed, _ := jsonpath.Get(fmt.Sprintf("$.value[%d].consumedUnits", i), v)
				metric := createMetricWithThresholds(
					sku.(string),
					".licences.consumed",
					consumed.(float64),
					float64(definition.WarningThreshold),
					float64(definition.CriticalThreshold),
				)
				service.Metrics = append(service.Metrics, *metric)
			}
		}
	}

	return
}
