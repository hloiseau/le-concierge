package monitoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestToparrSourceIsolationFreshnessAndCache(t *testing.T) {
	for _, mode := range []string{"available", "stale", "error", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			at := now.Add(-20 * time.Second)
			if mode == "stale" {
				at = now.Add(-31 * time.Second)
			}
			var calls atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "error" {
					w.WriteHeader(503)
					return
				}
				if r.URL.Path == "/api/gpus" {
					_, _ = w.Write([]byte(`{"gpus":[{"device":"card0"}],"current_device":null}`))
					return
				}
				var current any
				if mode != "unavailable" {
					current = map[string]any{"timestamp": float64(at.UnixMilli()) / 1000, "frequency": map[string]any{"actual": 0}, "engines": map[string]any{}, "power": map[string]any{"GPU": 14.7}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"current": current})
			}))
			defer source.Close()
			path := filepath.Join(t.TempDir(), "host.json")
			writeHost(t, path, hostFixture(now))
			service := New(upstream.New(), nil)
			service.now = func() time.Time { return now }
			c := config.Config{MonitoringEnabled: true, MetricsFile: path, ToparrURL: source.URL}
			got, err := service.Current(context.Background(), c)
			if err != nil || got.ToparrStatus != mode || got.HostStatus != "available" || got.Metrics["cpu_percent"].Value == nil {
				t.Fatalf("source failure leaked to host or wrong status: %s / %s, %v", got.ToparrStatus, got.HostStatus, err)
			}
			if mode == "available" {
				if got.ToparrUpdatedAt == nil || !got.ExpiresAt.Equal(now.Add(30*time.Second)) || got.Metrics["gpu_frequency_mhz"].Value == nil || *got.Metrics["gpu_frequency_mhz"].Value != 0 || got.Metrics["gpu_video_percent"].Status != "unavailable" || *got.Metrics["gpu_power_watts"].Value != 14.7 {
					t.Fatal("wrong GPU data or freshness")
				}
			} else if got.Metrics["gpu_power_watts"].Value != nil || got.Metrics["gpu_power_watts"].Status != mode {
				t.Fatal("retained unavailable GPU number")
			}
			before := calls.Load()
			if _, err = service.Current(context.Background(), c); err != nil || calls.Load() != before {
				t.Fatal("cache did not coalesce requests")
			}
			if mode == "available" {
				service.now = func() time.Time { return now.Add(11 * time.Second) }
				got, err = service.Current(context.Background(), c)
				if err != nil || calls.Load() != before+2 || got.ToparrStatus != "stale" || got.Metrics["gpu_power_watts"].Value != nil || got.Metrics["cpu_percent"].Status != "available" {
					t.Fatal("GPU cache outlived source or expired unrelated CPU data")
				}
			}
			c.ToparrURL = ""
			got, err = service.Current(context.Background(), c)
			if err != nil || got.ToparrStatus != "not_configured" || got.ToparrUpdatedAt != nil || got.Metrics["gpu_power_watts"].Value != nil {
				t.Fatal("configuration change retained Toparr data")
			}
		})
	}
}

func TestToparrRechecksFreshnessAfterOtherSources(t *testing.T) {
	now := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/toparr/api/gpus":
			_, _ = w.Write([]byte(`{"gpus":[{"device":"card0"}],"current_device":null}`))
		case "/toparr/api/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"current": map[string]any{"timestamp": float64(now.Add(-29*time.Second).UnixMilli()) / 1000, "power": map[string]any{"GPU": 14.7}}})
		default:
			var data any
			switch r.URL.Query().Get("cmd") {
			case "get_tautulli_info":
				data = map[string]any{"tautulli_version": "v2.18.2"}
			case "get_server_info":
				data = map[string]any{"pms_identifier": "server-a"}
			case "get_activity":
				clock.Add(int64(2 * time.Second))
				data = map[string]any{"stream_count": 0, "sessions": []any{}}
			default:
				t.Errorf("unexpected request %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"result": "success", "data": data}})
		}
	}))
	defer source.Close()
	service := New(upstream.New(), nil)
	service.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c := config.Config{MonitoringEnabled: true, ToparrURL: source.URL + "/toparr", TautulliURL: source.URL, TautulliAPIKey: "fixture-key", PlexServerID: "server-a"}
	got, err := service.Current(context.Background(), c)
	if err != nil || got.ToparrStatus != "stale" || got.Metrics["gpu_power_watts"].Value != nil || got.Activity.Status != "available" || !got.ExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatal("old GPU data stayed current after another source completed", err)
	}
}
