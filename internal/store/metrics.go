package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
)

const metricsSchema = `CREATE TABLE metric_samples(at INTEGER PRIMARY KEY, payload TEXT NOT NULL CHECK(length(payload)<=8192 AND json_valid(payload)));`

type MetricValue struct {
	Value  *float64 `json:"value"`
	Status string   `json:"status"`
	Unit   string   `json:"unit"`
}
type MetricSample struct {
	At     time.Time              `json:"at"`
	Values map[string]MetricValue `json:"values"`
}

var MetricUnits = map[string]string{
	"cpu_percent": "percent", "cpu_temperature": "celsius", "load_1m": "load",
	"memory_used_bytes": "bytes", "memory_total_bytes": "bytes",
	"gpu_video_percent": "percent", "gpu_render_percent": "percent", "gpu_temperature": "celsius", "gpu_power_watts": "watts", "gpu_frequency_mhz": "mhz",
	"disk_read_bytes_per_second": "bytes_per_second", "disk_write_bytes_per_second": "bytes_per_second",
	"network_receive_bytes_per_second": "bytes_per_second", "network_send_bytes_per_second": "bytes_per_second",
	"storage_used_bytes": "bytes", "storage_total_bytes": "bytes",
	"plex_sessions": "count", "plex_transcodes": "count", "plex_hardware_transcodes": "count",
}

func MissingMetrics(status string) map[string]MetricValue {
	out := map[string]MetricValue{}
	for key, unit := range MetricUnits {
		out[key] = MetricValue{Status: status, Unit: unit}
	}
	return out
}

func ValidateMetricSample(s MetricSample) error {
	if s.At.IsZero() || len(s.Values) != len(MetricUnits) {
		return errors.New("invalid metric sample")
	}
	for key, m := range s.Values {
		unit, exists := MetricUnits[key]
		if !exists || m.Unit != unit {
			return errors.New("unknown metric or unit")
		}
		if m.Status != "available" && m.Status != "unavailable" && m.Status != "error" && m.Status != "stale" {
			return errors.New("invalid metric status")
		}
		if (m.Status == "available") != (m.Value != nil) {
			return errors.New("inconsistent metric value")
		}
		if m.Value == nil {
			continue
		}
		v := *m.Value
		if math.IsNaN(v) || math.IsInf(v, 0) || v > 1e15 || v < 0 && unit != "celsius" || unit == "percent" && v > 100 || unit == "celsius" && (v < -273.15 || v > 1000) || unit == "count" && math.Trunc(v) != v {
			return errors.New("invalid metric value")
		}
	}
	return nil
}

// One bounded aggregate sample per minute; no session names or host identifiers.
func (s *Store) SaveMetricSample(ctx context.Context, sample MetricSample) error {
	if err := ValidateMetricSample(sample); err != nil {
		return err
	}
	data, err := json.Marshal(sample.Values)
	if err != nil || len(data) > 8192 {
		return errors.New("metric sample too large")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO metric_samples(at,payload) VALUES(?,?) ON CONFLICT(at) DO UPDATE SET payload=excluded.payload`, sample.At.Truncate(time.Minute).UnixMilli(), string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM metric_samples WHERE at<? OR at NOT IN (SELECT at FROM metric_samples ORDER BY at DESC LIMIT 10080)`, sample.At.Add(-7*24*time.Hour).UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// Retention continues even when collection is switched off.
func (s *Store) PruneMetricSamples(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM metric_samples WHERE at<?`, now.Add(-7*24*time.Hour).UnixMilli())
	return err
}

// MetricHistory returns at most 337 mean buckets; absent buckets remain gaps.
// A mixed/failed bucket is not averaged into a misleading healthy number.
func (s *Store) MetricHistory(ctx context.Context, now time.Time, window string) ([]MetricSample, int, error) {
	var duration, step time.Duration
	switch window {
	case "1h":
		duration = time.Hour
		step = time.Minute
	case "24h":
		duration = 24 * time.Hour
		step = 5 * time.Minute
	case "7d":
		duration = 7 * 24 * time.Hour
		step = 30 * time.Minute
	default:
		return nil, 0, errors.New("invalid metric history window")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at,payload FROM metric_samples WHERE at>=? AND at<=? ORDER BY at`, now.Add(-duration).UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []MetricSample{}
	counts := map[string]int{}
	var bucket *MetricSample
	var previousStamp int64
	gap := false
	flush := func() {
		if bucket == nil {
			return
		}
		for key, m := range bucket.Values {
			if gap && m.Status == "available" {
				m.Value = nil
				m.Status = "unavailable"
				bucket.Values[key] = m
			}
			if m.Value != nil {
				v := *m.Value / float64(counts[key])
				m.Value = &v
				bucket.Values[key] = m
			}
		}
		result = append(result, *bucket)
	}
	for rows.Next() {
		var stamp int64
		var data string
		if err := rows.Scan(&stamp, &data); err != nil {
			return nil, 0, err
		}
		var values map[string]MetricValue
		if json.Unmarshal([]byte(data), &values) != nil {
			return nil, 0, errors.New("invalid stored metric sample")
		}
		at := time.UnixMilli(stamp).UTC().Truncate(step)
		if bucket == nil || !at.Equal(bucket.At) {
			flush()
			bucket = &MetricSample{At: at, Values: values}
			previousStamp = stamp
			gap = false
			counts = map[string]int{}
			for key := range values {
				counts[key] = 1
			}
			continue
		}
		if stamp-previousStamp > time.Minute.Milliseconds() {
			gap = true
		}
		previousStamp = stamp
		for key, m := range values {
			previous := bucket.Values[key]
			counts[key]++
			if previous.Value != nil && m.Value != nil {
				v := *previous.Value + *m.Value
				previous.Value = &v
			} else {
				previous.Value = nil
				switch {
				case m.Status == "error" || previous.Status == "error":
					previous.Status = "error"
				case m.Status == "stale" || previous.Status == "stale":
					previous.Status = "stale"
				default:
					previous.Status = "unavailable"
				}
			}
			bucket.Values[key] = previous
		}
	}
	flush()
	return result, int(step.Seconds()), rows.Err()
}
