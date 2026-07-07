package collector

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"sentinel/agent/internal/models"
)

// collectNvidiaGPU shells out to nvidia-smi, which ships with the NVIDIA
// driver on both Windows and Linux and exposes the same CSV query interface
// on either OS. If nvidia-smi isn't on PATH (no NVIDIA GPU/driver), this
// silently returns no samples.
func collectNvidiaGPU(now time.Time) []models.GPUSample {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil
	}

	var samples []models.GPUSample
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 6 {
			continue
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		util, errU := strconv.ParseFloat(fields[2], 64)
		memUsedMiB, errMU := strconv.ParseFloat(fields[3], 64)
		memTotalMiB, errMT := strconv.ParseFloat(fields[4], 64)
		temp, errT := strconv.ParseFloat(fields[5], 64)

		sample := models.GPUSample{
			Time:     now,
			GPUIndex: idx,
			Vendor:   "nvidia",
			Name:     fields[1],
		}
		if errU == nil {
			sample.UtilizationPct = &util
		}
		if errMU == nil {
			b := int64(memUsedMiB * 1024 * 1024)
			sample.MemUsedBytes = &b
		}
		if errMT == nil {
			b := int64(memTotalMiB * 1024 * 1024)
			sample.MemTotalBytes = &b
		}
		if errT == nil {
			sample.TempC = &temp
		}
		samples = append(samples, sample)
	}
	return samples
}
