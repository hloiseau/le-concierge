package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToparrContractUnitsAbsenceAndPrivacy(t *testing.T) {
	for _, mode := range []string{"normal", "missing", "package_power", "starting", "ambiguous", "wrong_selection", "invalid_busy", "invalid_power", "invalid_timestamp", "future"} {
		t.Run(mode, func(t *testing.T) {
			sample := map[string]any{
				"timestamp": float64(time.Now().UnixMilli()) / 1000,
				"frequency": map[string]any{"actual": 300},
				"power":     map[string]any{"GPU": 14.7},
				"engines":   map[string]any{"Video/0": map[string]any{"busy": 12.4}, "Video/1": map[string]any{"busy": 35}, "Render/3D/0": map[string]any{"busy": 0}, "VideoEnhance/0": map[string]any{"busy": 100}},
				"clients":   map[string]any{"PRIVATE-ID": map[string]any{"pid": "PRIVATE-PID", "cmdline": "PRIVATE-COMMAND"}},
			}
			inventory := map[string]any{"gpus": []map[string]string{{"device": "card0", "name": "PRIVATE-GPU"}}, "current_device": nil}
			switch mode {
			case "missing":
				sample["engines"], sample["frequency"], sample["power"] = map[string]any{}, nil, nil
			case "package_power":
				sample["power"] = map[string]any{"GPU": nil, "PKG": 42}
			case "starting":
				sample = nil
			case "ambiguous":
				inventory["gpus"] = []map[string]string{{"device": "card0"}, {"device": "card1"}}
				inventory["current_device"] = "card0"
			case "wrong_selection":
				inventory["current_device"] = "card1"
			case "invalid_busy":
				sample["engines"] = map[string]any{"Video/0": map[string]any{"busy": 101}}
			case "invalid_power":
				sample["power"] = map[string]any{"GPU": -1}
			case "invalid_timestamp":
				delete(sample, "timestamp")
			case "future":
				sample["timestamp"] = time.Now().Add(time.Minute).Unix()
			}
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.RawQuery != "" {
					t.Error("unexpected Toparr mutation or query")
				}
				switch r.URL.Path {
				case "/toparr/api/gpus":
					_ = json.NewEncoder(w).Encode(inventory)
				case "/toparr/api/status":
					_ = json.NewEncoder(w).Encode(map[string]any{"current": sample, "history": []any{"PRIVATE-HISTORY"}, "gpu_name": "PRIVATE-GPU"})
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer source.Close()
			got, err := New().ToparrGPU(context.Background(), source.URL+"/toparr")
			if strings.HasPrefix(mode, "invalid") || mode == "future" || mode == "ambiguous" || mode == "wrong_selection" {
				if err == nil {
					t.Fatal("invalid or ambiguous GPU observation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "starting" {
				if !got.UpdatedAt.IsZero() {
					t.Fatal("invented starting observation")
				}
				return
			}
			if mode == "missing" {
				if got.VideoPercent != nil || got.RenderPercent != nil || got.FrequencyMHz != nil || got.PowerWatts != nil {
					t.Fatal("missing measurement converted to zero")
				}
				return
			}
			if got.VideoPercent == nil || *got.VideoPercent != 35 || got.RenderPercent == nil || *got.RenderPercent != 0 || got.FrequencyMHz == nil || *got.FrequencyMHz != 300 {
				t.Fatal("incorrect engine mapping, zero or units")
			}
			if mode == "package_power" {
				if got.PowerWatts != nil {
					t.Fatal("CPU package power reported as GPU power")
				}
			} else if got.PowerWatts == nil || *got.PowerWatts != 14.7 {
				t.Fatal("GPU watts lost")
			}
			serialized, _ := json.Marshal(got)
			if strings.Contains(string(serialized), "PRIVATE") {
				t.Fatal("private Toparr data retained")
			}
		})
	}
}
