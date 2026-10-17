package store

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func metricFixture(at time.Time, v float64) MetricSample {
	values := MissingMetrics("unavailable")
	values["cpu_percent"] = MetricValue{Value: &v, Status: "available", Unit: "percent"}
	return MetricSample{At: at, Values: values}
}

func TestMetricValidation(t *testing.T) {
	for _, mode := range []string{"zero", "missing", "nan", "percent", "unit", "unknown", "inconsistent"} {
		t.Run(mode, func(t *testing.T) {
			sample := metricFixture(time.Now(), 0)
			m := sample.Values["cpu_percent"]
			switch mode {
			case "missing":
				m.Value = nil
				m.Status = "unavailable"
			case "nan":
				*m.Value = math.NaN()
			case "percent":
				*m.Value = 101
			case "unit":
				m.Unit = "bytes"
			case "unknown":
				sample.Values["secret"] = m
			case "inconsistent":
				m.Status = "error"
			}
			sample.Values["cpu_percent"] = m
			err := ValidateMetricSample(sample)
			if (mode == "zero" || mode == "missing") != (err == nil) {
				t.Fatalf("unexpected validation %v", err)
			}
		})
	}
}

func TestMetricsHistoryPersistenceRetentionAndGaps(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metrics.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for i, v := range []float64{0, 20, 40} {
		if err = s.SaveMetricSample(ctx, metricFixture(at.Add(time.Duration(i)*time.Minute), v)); err != nil {
			t.Fatal(err)
		}
	}
	// A later observation replaces this minute, instead of double-counting it.
	if err = s.SaveMetricSample(ctx, metricFixture(at.Add(time.Minute+time.Second), 30)); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveMetricSample(ctx, metricFixture(at.Add(10*time.Minute), 70)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, step, err := s.MetricHistory(ctx, at.Add(11*time.Minute), "24h")
	if err != nil || step != 300 || len(history) != 2 {
		t.Fatalf("history %+v %d %v", history, step, err)
	}
	if math.Abs(*history[0].Values["cpu_percent"].Value-70.0/3) > 1e-9 || history[1].At.Sub(history[0].At) != 10*time.Minute {
		t.Fatal("mean/gap mismatch")
	}
	failed := metricFixture(at.Add(3*time.Minute), 0)
	failed.Values["cpu_percent"] = MetricValue{Status: "error", Unit: "percent"}
	if err = s.SaveMetricSample(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveMetricSample(ctx, metricFixture(at.Add(4*time.Minute), 50)); err != nil {
		t.Fatal(err)
	}
	history, _, err = s.MetricHistory(ctx, at.Add(11*time.Minute), "24h")
	if err != nil || history[0].Values["cpu_percent"].Value != nil || history[0].Values["cpu_percent"].Status != "error" {
		t.Fatal("failure disappeared during mean aggregation")
	}
	if err = s.SaveMetricSample(ctx, metricFixture(at.Add(8*24*time.Hour), 1)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM metric_samples`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retention count %d %v", count, err)
	}
	if _, _, err = s.MetricHistory(ctx, at, "forever"); err == nil {
		t.Fatal("unbounded query accepted")
	}
}

func TestMetricHistoryDetectsGapInsideMeanBucket(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, minutes := range []int{0, 2} {
		if err = s.SaveMetricSample(ctx, metricFixture(at.Add(time.Duration(minutes)*time.Minute), 50)); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := s.MetricHistory(ctx, at.Add(4*time.Minute), "24h")
	if err != nil || len(items) != 1 || items[0].Values["cpu_percent"].Value != nil {
		t.Fatal("collection gap hidden by mean")
	}
	// The raw minute view preserves real points on either side of the gap.
	items, _, err = s.MetricHistory(ctx, at.Add(4*time.Minute), "1h")
	if err != nil || len(items) != 2 || *items[0].Values["cpu_percent"].Value != 50 {
		t.Fatal("raw observations lost")
	}
}

func TestMetricRowsHaveHardBudget(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	// Correlated writers and clock changes cannot retain more than 10,080 rows.
	if _, err = s.DB.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<10100) INSERT INTO metric_samples(at,payload) SELECT ?, '{}' FROM seq`, now.UnixMilli()); err == nil {
		t.Fatal("duplicate primary keys accepted")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10100; i++ {
		if _, err = tx.Exec(`INSERT INTO metric_samples(at,payload) VALUES(?, '{}')`, now.Add(-time.Duration(i)*time.Second).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveMetricSample(ctx, metricFixture(now, 0)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM metric_samples`).Scan(&count); err != nil || count != 10080 {
		t.Fatalf("unbounded history %d %v", count, err)
	}
}
