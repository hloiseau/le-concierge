package monitoring

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type HostDetail struct {
	Kind   string                       `json:"kind"`
	Name   string                       `json:"name"`
	Status string                       `json:"status"`
	Values map[string]store.MetricValue `json:"values"`
}
type HostSnapshot struct {
	Version     int                          `json:"version"`
	CollectedAt time.Time                    `json:"collectedAt"`
	HostVersion string                       `json:"hostVersion"`
	Values      map[string]store.MetricValue `json:"values"`
	Details     []HostDetail                 `json:"details"`
}
type Snapshot struct {
	UpdatedAt         time.Time                    `json:"updatedAt"`
	ExpiresAt         time.Time                    `json:"expiresAt"`
	HostUpdatedAt     *time.Time                   `json:"hostUpdatedAt"`
	ActivityUpdatedAt *time.Time                   `json:"activityUpdatedAt"`
	Metrics           map[string]store.MetricValue `json:"metrics"`
	Activity          upstream.ActivitySummary     `json:"activity"`
	HostVersion       string                       `json:"-"`
	HostDetails       []HostDetail                 `json:"-"`
	Sessions          []upstream.ActivitySession   `json:"-"`
}
type Service struct {
	client *upstream.Client
	db     *store.Store
	gate   chan struct{}
	mu     sync.Mutex
	key    [32]byte
	cached Snapshot
}

func New(client *upstream.Client, db *store.Store) *Service {
	return &Service{client: client, db: db, gate: make(chan struct{}, 1)}
}

func (s *Service) Current(ctx context.Context, c config.Config) (Snapshot, error) {
	if !c.MonitoringEnabled {
		return Snapshot{}, errors.New("monitoring disabled")
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	key := sha256.Sum256([]byte(c.MetricsFile + "\x00" + c.TautulliURL + "\x00" + c.TautulliAPIKey + "\x00" + c.PlexServerID))
	s.mu.Lock()
	cached := s.cached
	same := s.key == key
	s.mu.Unlock()
	if same && time.Now().Before(cached.ExpiresAt) {
		return cached, nil
	}
	now := time.Now().UTC()
	out := Snapshot{UpdatedAt: now, ExpiresAt: now.Add(30 * time.Second), Metrics: store.MissingMetrics("unavailable"), Activity: upstream.ActivitySummary{Status: "unavailable"}, Sessions: []upstream.ActivitySession{}, HostDetails: []HostDetail{}}
	if c.MetricsFile != "" {
		host, err := readHost(c.MetricsFile, now)
		if err != nil {
			for key, m := range out.Metrics {
				if !strings.HasPrefix(key, "plex_") {
					m.Status = "error"
					out.Metrics[key] = m
				}
			}
		} else {
			out.HostUpdatedAt = &host.CollectedAt
			out.HostVersion = host.HostVersion
			out.HostDetails = host.Details
			stale := now.Sub(host.CollectedAt) > 2*time.Minute
			if !stale && host.CollectedAt.Add(2*time.Minute).Before(out.ExpiresAt) {
				out.ExpiresAt = host.CollectedAt.Add(2 * time.Minute)
			}
			for key, m := range host.Values {
				if strings.HasPrefix(key, "plex_") {
					continue
				}
				if stale {
					m.Value = nil
					m.Status = "stale"
				}
				out.Metrics[key] = m
			}
			if stale {
				out.HostDetails = []HostDetail{}
			}
		}
	}
	if c.TautulliURL != "" && c.TautulliAPIKey != "" && c.PlexServerID != "" {
		activity, err := s.client.TautulliActivity(ctx, c.TautulliURL, c.TautulliAPIKey, c.PlexServerID)
		if err != nil {
			out.Activity.Status = "error"
			for key, m := range out.Metrics {
				if strings.HasPrefix(key, "plex_") {
					m.Status = "error"
					out.Metrics[key] = m
				}
			}
		} else {
			out.Activity = activity.Summary
			out.Sessions = activity.Sessions
			out.ActivityUpdatedAt = &activity.UpdatedAt
			for key, p := range map[string]*int{"plex_sessions": activity.Summary.Sessions, "plex_transcodes": activity.Summary.Transcodes, "plex_hardware_transcodes": activity.Summary.HardwareTranscodes} {
				if p != nil {
					v := float64(*p)
					out.Metrics[key] = store.MetricValue{Value: &v, Status: "available", Unit: "count"}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	s.mu.Lock()
	s.key = key
	s.cached = out
	s.mu.Unlock()
	return out, nil
}

func (s *Service) Sample(ctx context.Context, c config.Config) error {
	snapshot, err := s.Current(ctx, c)
	if err != nil {
		return err
	}
	return s.db.SaveMetricSample(ctx, store.MetricSample{At: snapshot.UpdatedAt, Values: snapshot.Metrics})
}

func readHost(path string, now time.Time) (HostSnapshot, error) {
	pre, err := os.Stat(path)
	if err != nil || !pre.Mode().IsRegular() {
		return HostSnapshot{}, errors.New("invalid metrics source")
	}
	f, err := os.Open(path)
	if err != nil {
		return HostSnapshot{}, errors.New("metrics source unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32768 {
		return HostSnapshot{}, errors.New("invalid metrics source")
	}
	var host HostSnapshot
	d := json.NewDecoder(io.LimitReader(f, 32769))
	d.DisallowUnknownFields()
	if d.Decode(&host) != nil || d.Decode(new(any)) != io.EOF || host.Version != 1 || host.CollectedAt.IsZero() || host.CollectedAt.After(now.Add(15*time.Second)) || len(host.HostVersion) > 100 || len(host.Details) > 32 {
		return HostSnapshot{}, errors.New("invalid metrics source")
	}
	values := store.MissingMetrics("unavailable")
	for key, value := range host.Values {
		if _, ok := store.MetricUnits[key]; !ok || strings.HasPrefix(key, "plex_") {
			return HostSnapshot{}, errors.New("invalid host metric")
		}
		values[key] = value
	}
	if err := store.ValidateMetricSample(store.MetricSample{At: host.CollectedAt, Values: values}); err != nil {
		return HostSnapshot{}, err
	}
	for _, detail := range host.Details {
		if len(detail.Name) == 0 || len(detail.Name) > 128 || strings.ContainsAny(detail.Name, "\r\n\x00") || (detail.Kind != "disk" && detail.Kind != "pool" && detail.Kind != "interface" && detail.Kind != "gpu") || (detail.Status != "available" && detail.Status != "unavailable" && detail.Status != "error") {
			return HostSnapshot{}, errors.New("invalid host detail")
		}
		check := store.MissingMetrics("unavailable")
		for key, value := range detail.Values {
			if _, ok := store.MetricUnits[key]; !ok || strings.HasPrefix(key, "plex_") {
				return HostSnapshot{}, errors.New("invalid host detail metric")
			}
			check[key] = value
		}
		if err := store.ValidateMetricSample(store.MetricSample{At: host.CollectedAt, Values: check}); err != nil {
			return HostSnapshot{}, err
		}
	}
	host.Values = values
	return host, nil
}
