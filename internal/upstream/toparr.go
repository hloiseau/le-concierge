package upstream

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"
)

// GPUObservation excludes Toparr's process names, command lines and history.
type GPUObservation struct {
	UpdatedAt     time.Time
	VideoPercent  *float64
	RenderPercent *float64
	FrequencyMHz  *float64
	PowerWatts    *float64
}

// ToparrGPU reads the sysfs/fdinfo contract shipped by Toparr 2.0.1.
func (c *Client) ToparrGPU(ctx context.Context, base string) (GPUObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var inventory struct {
		GPUs []struct {
			Device string `json:"device"`
		} `json:"gpus"`
		Current *string `json:"current_device"`
	}
	if err := c.JSON(ctx, http.MethodGet, base+"/api/gpus", nil, "", &inventory); err != nil {
		return GPUObservation{}, err
	}
	// shortcut: 2.0.1 omits drivers and mixes same-driver clients; accept more
	// devices when Toparr can prove that its observations belong to one GPU.
	if len(inventory.GPUs) != 1 || inventory.GPUs[0].Device == "" || (inventory.Current != nil && *inventory.Current != inventory.GPUs[0].Device) {
		return GPUObservation{}, ErrIncomplete
	}
	var input struct {
		Current *struct {
			Timestamp float64 `json:"timestamp"`
			Frequency struct {
				Actual *float64 `json:"actual"`
			} `json:"frequency"`
			Power struct {
				GPU *float64 `json:"GPU"`
			} `json:"power"`
			Engines map[string]struct {
				Busy *float64 `json:"busy"`
			} `json:"engines"`
		} `json:"current"`
	}
	if err := c.JSON(ctx, http.MethodGet, base+"/api/status", nil, "", &input); err != nil {
		return GPUObservation{}, err
	}
	if input.Current == nil {
		return GPUObservation{}, nil
	}
	sample := input.Current
	if sample.Timestamp <= 0 || math.IsNaN(sample.Timestamp) || math.IsInf(sample.Timestamp, 0) || sample.Timestamp > float64(time.Now().Add(15*time.Second).Unix()) || len(sample.Engines) > 64 {
		return GPUObservation{}, ErrIncomplete
	}
	seconds, fraction := math.Modf(sample.Timestamp)
	out := GPUObservation{UpdatedAt: time.Unix(int64(seconds), int64(fraction*1e9)).UTC(), FrequencyMHz: sample.Frequency.Actual, PowerWatts: sample.Power.GPU}
	for name, engine := range sample.Engines {
		var target **float64
		switch {
		case name == "Video" || strings.HasPrefix(name, "Video/"):
			target = &out.VideoPercent
		case name == "Render/3D" || strings.HasPrefix(name, "Render/3D/"):
			target = &out.RenderPercent
		default:
			continue
		}
		if !validGPUValue(engine.Busy, 100) {
			return GPUObservation{}, ErrIncomplete
		}
		if engine.Busy != nil && (*target == nil || **target < *engine.Busy) {
			*target = engine.Busy
		}
	}
	if !validGPUValue(out.FrequencyMHz, 1e6) || !validGPUValue(out.PowerWatts, 1e6) {
		return GPUObservation{}, ErrIncomplete
	}
	return out, nil
}

func validGPUValue(value *float64, maximum float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= maximum)
}
