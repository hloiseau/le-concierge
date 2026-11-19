package httpapi

import (
	"encoding/json"
	"strings"

	"testing"

	"github.com/hloiseau/le-concierge/internal/store"
)

func TestErisMetricsNeverInventMissingOrStaleMeasurements(t *testing.T) {
	v := 42.0
	got := erisMetrics(map[string]store.MetricValue{
		"cpu_percent":       {Status: "available", Value: &v},
		"cpu_temperature":   {Status: "stale", Value: &v},
		"memory_used_bytes": {Status: "available"},
		"private_metric":    {Status: "available", Value: &v},
	})
	var facts struct{ Metrics map[string]store.MetricValue }
	if json.Unmarshal([]byte(got), &facts) != nil || facts.Metrics["cpu_percent"].Value == nil || *facts.Metrics["cpu_percent"].Value != 42 || facts.Metrics["cpu_temperature"].Status != "stale" || facts.Metrics["cpu_temperature"].Value != nil || facts.Metrics["memory_used_bytes"].Value != nil || strings.Contains(got, "private_metric") {
		t.Fatal(got)
	}

}
