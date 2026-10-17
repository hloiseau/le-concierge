package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTautulliActivityPrivacyAndUnits(t *testing.T) {
	for _, mode := range []string{"normal", "empty", "unknown_hw", "unknown_bandwidth", "invalid_state", "invalid_decision", "invalid_count", "wrong_server", "wrong_version"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-Key") != "secret" || strings.Contains(r.URL.String(), "secret") {
					t.Error("unsafe request")
				}
				var data any
				switch r.URL.Query().Get("cmd") {
				case "get_tautulli_info":
					version := "v2.18.2"
					if mode == "wrong_version" {
						version = "v2.19.0"
					}
					data = map[string]any{"tautulli_version": version}
				case "get_server_info":
					id := "server"
					if mode == "wrong_server" {
						id = "other"
					}
					data = map[string]any{"pms_identifier": id}
				case "get_activity":
					calls++
					sessions := []map[string]any{}
					for _, decision := range []string{"direct play", "copy", "transcode"} {
						sessions = append(sessions, map[string]any{"friendly_name": "Alice\nExample", "full_title": "Private title", "state": "playing", "transcode_decision": decision, "stream_video_codec": "hevc", "stream_audio_codec": "aac", "bandwidth": "1000", "transcode_hw_decoding": "1", "transcode_hw_encoding": 0, "ip_address": "secret-ip", "session_key": "secret-key", "file": "secret-path"})
					}
					count := 3
					switch mode {
					case "empty":
						sessions = []map[string]any{}
						count = 0
					case "unknown_hw":
						delete(sessions[2], "transcode_hw_decoding")
					case "unknown_bandwidth":
						delete(sessions[0], "bandwidth")
					case "invalid_state":
						sessions[0]["state"] = "surprise"
					case "invalid_decision":
						sessions[0]["transcode_decision"] = "surprise"
					case "invalid_count":
						count = 1
					}
					data = map[string]any{"stream_count": count, "sessions": sessions}
				default:
					t.Error("unexpected command")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"result": "success", "data": data}})
			}))
			defer source.Close()
			got, err := New().TautulliActivity(context.Background(), source.URL, "secret", "server")
			if strings.HasPrefix(mode, "invalid") || strings.HasPrefix(mode, "wrong") {
				if err == nil {
					t.Fatal("invalid activity accepted")
				}
				if strings.HasPrefix(mode, "wrong") && calls != 0 {
					t.Fatal("activity fetched without verified source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			summary, _ := json.Marshal(got.Summary)
			if strings.Contains(string(summary), "Alice") || strings.Contains(string(summary), "Private") {
				t.Fatal("aggregate reveals identity")
			}
			encoded, _ := json.Marshal(got.Sessions)
			if strings.Contains(string(encoded), "secret") {
				t.Fatal("session leaks raw fields")
			}
			if mode == "empty" {
				if *got.Summary.Sessions != 0 || *got.Summary.BandwidthKbps != 0 || *got.Summary.HardwareTranscodes != 0 {
					t.Fatal("zero sessions must remain a real zero")
				}
				return
			}
			if *got.Summary.Sessions != 3 || *got.Summary.DirectPlay != 1 || *got.Summary.DirectStream != 1 || *got.Summary.Transcodes != 1 || got.Sessions[0].User != "Alice Example" {
				t.Fatalf("wrong summary %+v", got)
			}
			if mode == "unknown_hw" {
				if got.Summary.HardwareTranscodes != nil {
					t.Fatal("unknown hardware became zero")
				}
			} else if *got.Summary.HardwareTranscodes != 1 {
				t.Fatal("hardware count inferred incorrectly")
			}
			if mode == "unknown_bandwidth" {
				if got.Summary.BandwidthKbps != nil {
					t.Fatal("missing bandwidth became zero")
				}
			} else if *got.Summary.BandwidthKbps != 3000 {
				t.Fatal("bandwidth must remain kbps")
			}
		})
	}
}
