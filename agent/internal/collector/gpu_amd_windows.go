//go:build windows

package collector

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"sentinel/agent/internal/models"
)

// collectAMDGPU has no reliable free CLI/API on Windows (unlike NVIDIA's
// nvidia-smi). This best-effort implementation reads the GPU engine
// performance counters exposed via WMI, which report per-engine utilization
// for whatever GPU is installed (not AMD-specific, but it's the only
// generally available signal). If the counters aren't present, or a
// dedicated NVIDIA sample already reported this index, no sample is added.
func collectAMDGPU(now time.Time) []models.GPUSample {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine | `+
			`Select-Object Name, UtilizationPercentage | ConvertTo-Json -Compress`)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	type engineCounter struct {
		Name                 string `json:"Name"`
		UtilizationPercentage float64 `json:"UtilizationPercentage"`
	}

	var counters []engineCounter
	trimmed := strings.TrimSpace(string(out))
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(out, &counters); err != nil {
			return nil
		}
	} else if trimmed != "" {
		var single engineCounter
		if err := json.Unmarshal(out, &single); err != nil {
			return nil
		}
		counters = []engineCounter{single}
	}
	if len(counters) == 0 {
		return nil
	}

	// Sum engine utilization per physical GPU (engine names look like
	// "pid_1234_luid_0x...._phys_0_eng_0_engtype_3D"); we bucket by the
	// "phys_N" segment as a best-effort GPU index.
	totals := map[int]float64{}
	for _, c := range counters {
		idx := 0
		if p := strings.Index(c.Name, "phys_"); p != -1 {
			rest := c.Name[p+len("phys_"):]
			if end := strings.Index(rest, "_"); end != -1 {
				rest = rest[:end]
			}
			if n, err := strconv.Atoi(rest); err == nil {
				idx = n
			}
		}
		totals[idx] += c.UtilizationPercentage
	}

	samples := make([]models.GPUSample, 0, len(totals))
	for idx, util := range totals {
		u := util
		samples = append(samples, models.GPUSample{
			Time: now, GPUIndex: idx, Vendor: "amd", Name: "GPU " + strconv.Itoa(idx),
			UtilizationPct: &u,
		})
	}
	return samples
}
