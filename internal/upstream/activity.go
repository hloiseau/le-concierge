package upstream

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ActivitySummary struct {
	Status             string   `json:"status"`
	Sessions           *int     `json:"sessions"`
	DirectPlay         *int     `json:"directPlay"`
	DirectStream       *int     `json:"directStream"`
	Transcodes         *int     `json:"transcodes"`
	HardwareTranscodes *int     `json:"hardwareTranscodes"`
	BandwidthKbps      *float64 `json:"bandwidthKbps"`
}

// ActivitySession intentionally excludes addresses, paths, tokens and raw IDs.
type ActivitySession struct {
	User             string   `json:"user"`
	Title            string   `json:"title"`
	State            string   `json:"state"`
	Decision         string   `json:"decision"`
	VideoCodec       string   `json:"videoCodec"`
	AudioCodec       string   `json:"audioCodec"`
	BandwidthKbps    *float64 `json:"bandwidthKbps"`
	HardwareDecoding *bool    `json:"hardwareDecoding"`
	HardwareEncoding *bool    `json:"hardwareEncoding"`
}

type Activity struct {
	Summary   ActivitySummary
	Sessions  []ActivitySession
	UpdatedAt time.Time
}

// TautulliActivity only calls read commands for the pinned source version and
// server. Hardware flags are Plex pipeline observations, never GPU utilization.
func (c *Client) TautulliActivity(ctx context.Context, base, key, serverID string) (Activity, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := c.TautulliStatus(ctx, base, key, serverID); err != nil {
		return Activity{}, err
	}
	var input struct {
		Count    json.RawMessage `json:"stream_count"`
		Sessions []struct {
			User       string          `json:"friendly_name"`
			Title      string          `json:"full_title"`
			State      string          `json:"state"`
			Decision   string          `json:"transcode_decision"`
			VideoCodec string          `json:"stream_video_codec"`
			AudioCodec string          `json:"stream_audio_codec"`
			Bandwidth  json.RawMessage `json:"bandwidth"`
			HWDecode   json.RawMessage `json:"transcode_hw_decoding"`
			HWEncode   json.RawMessage `json:"transcode_hw_encoding"`
		} `json:"sessions"`
	}
	if err := c.tautulli(ctx, base, key, "get_activity", nil, &input); err != nil {
		return Activity{}, err
	}
	count := activityNumber(input.Count)
	if input.Sessions == nil || len(input.Sessions) > 500 || count == nil || *count < float64(len(input.Sessions)) || *count > 1000 || math.Trunc(*count) != *count {
		return Activity{}, ErrIncomplete
	}
	out := Activity{Sessions: []ActivitySession{}, UpdatedAt: time.Now().UTC()}
	total, direct, copying, transcodes, hardware := len(input.Sessions), 0, 0, 0, 0
	bandwidth := 0.0
	knownBandwidth, knownHardware := true, true
	for _, s := range input.Sessions {
		if s.Decision != "direct play" && s.Decision != "copy" && s.Decision != "transcode" {
			return Activity{}, ErrIncomplete
		}
		if s.State != "playing" && s.State != "paused" && s.State != "buffering" {
			return Activity{}, ErrIncomplete
		}
		bw := activityNumber(s.Bandwidth)
		if bw != nil && (*bw < 0 || *bw > 1e9) {
			return Activity{}, ErrIncomplete
		}
		if bw == nil {
			knownBandwidth = false
		} else {
			bandwidth += *bw
		}
		decode, encode := activityBool(s.HWDecode), activityBool(s.HWEncode)
		switch s.Decision {
		case "direct play":
			direct++
		case "copy":
			copying++
		case "transcode":
			transcodes++
			if decode != nil && *decode || encode != nil && *encode {
				hardware++
			} else if decode == nil || encode == nil {
				knownHardware = false
			}
		}
		out.Sessions = append(out.Sessions, ActivitySession{cleanActivity(s.User, 100), cleanActivity(s.Title, 200), s.State, s.Decision, cleanActivity(s.VideoCodec, 30), cleanActivity(s.AudioCodec, 30), bw, decode, encode})
	}
	out.Summary = ActivitySummary{Status: "available", Sessions: &total, DirectPlay: &direct, DirectStream: &copying, Transcodes: &transcodes}
	if knownHardware {
		out.Summary.HardwareTranscodes = &hardware
	}
	if knownBandwidth {
		out.Summary.BandwidthKbps = &bandwidth
	}
	return out, nil
}

func activityNumber(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return nil
	}
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
func activityBool(raw json.RawMessage) *bool {
	n := activityNumber(raw)
	if n == nil || (*n != 0 && *n != 1) {
		return nil
	}
	v := *n == 1
	return &v
}
func cleanActivity(s string, limit int) string {
	s = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, s)
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return strings.TrimSpace(s)
}
