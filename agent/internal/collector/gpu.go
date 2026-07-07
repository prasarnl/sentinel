package collector

import (
	"time"

	"sentinel/agent/internal/models"
)

// collectGPU merges NVIDIA (cross-platform via nvidia-smi) and AMD
// (platform-specific, see gpu_amd_*.go) samples. Any vendor whose tooling
// isn't present on this host simply contributes nothing rather than
// erroring, since GPU monitoring is inherently best-effort across such a
// varied hardware/driver landscape.
func (c *Collector) collectGPU(now time.Time) []models.GPUSample {
	samples := collectNvidiaGPU(now)
	samples = append(samples, collectAMDGPU(now)...)
	return samples
}
