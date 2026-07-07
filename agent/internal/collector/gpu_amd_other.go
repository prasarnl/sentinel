//go:build !linux && !windows

package collector

import (
	"time"

	"sentinel/agent/internal/models"
)

// collectAMDGPU has no supported collection path on platforms other than
// Linux (rocm-smi) and Windows (WMI); this build target exists so the
// agent still compiles for local development on e.g. macOS.
func collectAMDGPU(now time.Time) []models.GPUSample {
	return nil
}
