package monitoring

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func hostFixture(at time.Time) HostSnapshot {
	zero := 0.0
	return HostSnapshot{Version: 1, CollectedAt: at, HostVersion: "25.10 fixture", Values: map[string]store.MetricValue{"cpu_percent": {Value: &zero, Status: "available", Unit: "percent"}}, Details: []HostDetail{{Kind: "pool", Name: "private-pool", Status: "available", Values: map[string]store.MetricValue{}}}}
}
func writeHost(t *testing.T, path string, host HostSnapshot) {
	t.Helper()
	b, err := json.Marshal(host)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringFreshnessUnknownAndPrivacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	now := time.Now().UTC()
	writeHost(t, path, hostFixture(now.Add(-100*time.Second)))
	service := New(upstream.New(), nil)
	c := config.Config{MonitoringEnabled: true, MetricsFile: path}
	got, err := service.Current(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if got.HostStatus != "available" || got.Metrics["cpu_percent"].Value == nil || *got.Metrics["cpu_percent"].Value != 0 || got.Metrics["gpu_video_percent"].Value != nil || got.Metrics["gpu_video_percent"].Status != "unavailable" {
		t.Fatal("zero and unsupported conflated")
	}
	if got.ExpiresAt.After(now.Add(21 * time.Second)) {
		t.Fatal("cache outlives source freshness")
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "private-pool") || strings.Contains(string(b), "25.10") {
		t.Fatal("public snapshot exposes host details")
	}
	writeHost(t, path, hostFixture(now.Add(-3*time.Minute)))
	service = New(upstream.New(), nil)
	got, err = service.Current(context.Background(), c)
	if err != nil || got.HostStatus != "stale" || got.Metrics["cpu_percent"].Status != "stale" || got.Metrics["cpu_percent"].Value != nil || len(got.HostDetails) != 0 {
		t.Fatal("stale data retained")
	}
	if err = os.WriteFile(path, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	service = New(upstream.New(), nil)
	got, err = service.Current(context.Background(), c)
	if err != nil || got.HostStatus != "error" || got.Metrics["cpu_percent"].Status != "error" {
		t.Fatal("malformed source not error")
	}
	c.MetricsFile = ""
	got, err = service.Current(context.Background(), c)
	if err != nil || got.HostStatus != "not_configured" || got.Metrics["cpu_percent"].Status != "unavailable" {
		t.Fatal("configuration change reused cache")
	}
	c.MonitoringEnabled = false
	if _, err = service.Current(context.Background(), c); err == nil {
		t.Fatal("disabled monitoring fetched")
	}
}

func TestHostBoundary(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "source.json")
	for _, mode := range []string{"future", "version", "unknown_metric", "wrong_unit", "private_metric", "too_many_details", "bad_name", "trailing", "large"} {
		t.Run(mode, func(t *testing.T) {
			h := hostFixture(now)
			switch mode {
			case "future":
				h.CollectedAt = now.Add(time.Hour)
			case "version":
				h.Version = 2
			case "unknown_metric":
				h.Values["unknown"] = store.MetricValue{}
			case "wrong_unit":
				m := h.Values["cpu_percent"]
				m.Unit = "celsius"
				h.Values["cpu_percent"] = m
			case "private_metric":
				h.Values["plex_sessions"] = store.MetricValue{Status: "unavailable", Unit: "count"}
			case "too_many_details":
				h.Details = make([]HostDetail, 33)
			case "bad_name":
				h.Details[0].Name = "a\nb"
			}
			writeHost(t, path, h)
			if mode == "trailing" {
				f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				_, _ = f.WriteString(" {}")
				f.Close()
			}
			if mode == "large" {
				_ = os.WriteFile(path, []byte(strings.Repeat(" ", 32769)), 0600)
			}
			if _, err := readHost(path, now); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	if _, err := readHost(filepath.Dir(path), now); err == nil {
		t.Fatal("directory accepted")
	}
}
