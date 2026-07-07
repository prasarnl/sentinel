//go:build linux

package collector

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sentinel/agent/internal/models"
)

var cardIndexRe = regexp.MustCompile(`\d+`)

// collectAMDGPU shells out to ROCm's rocm-smi CLI, present when the ROCm
// stack is installed on Linux. Output field names have shifted across ROCm
// versions, so keys are matched by substring rather than an exact schema;
// if rocm-smi isn't installed or its output doesn't parse, this returns no
// samples rather than erroring.
func collectAMDGPU(now time.Time) []models.GPUSample {
	out, err := exec.Command("rocm-smi", "--showid", "--showuse", "--showmemuse", "--showtemp", "--json").Output()
	if err != nil {
		return nil
	}

	var raw map[string]map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	var samples []models.GPUSample
	for card, fields := range raw {
		idxMatch := cardIndexRe.FindString(card)
		idx, _ := strconv.Atoi(idxMatch)

		sample := models.GPUSample{Time: now, GPUIndex: idx, Vendor: "amd", Name: card}
		for key, val := range fields {
			lower := strings.ToLower(key)
			switch {
			case strings.Contains(lower, "gpu use"):
				if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
					sample.UtilizationPct = &f
				}
			case strings.Contains(lower, "vram total used"):
				if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
					b := int64(f)
					sample.MemUsedBytes = &b
				}
			case strings.Contains(lower, "vram total memory"):
				if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
					b := int64(f)
					sample.MemTotalBytes = &b
				}
			case strings.Contains(lower, "temperature"):
				if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
					sample.TempC = &f
				}
			}
		}
		samples = append(samples, sample)
	}
	return samples
}
