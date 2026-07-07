package collector

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"

	"sentinel/agent/internal/models"
)

func (c *Collector) collectDisk(now time.Time) []models.DiskSample {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil
	}

	ioCounters, _ := disk.IOCounters() // best-effort; nil map is fine

	samples := make([]models.DiskSample, 0, len(partitions))
	for _, p := range partitions {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}

		sample := models.DiskSample{
			Time:       now,
			Mount:      p.Mountpoint,
			TotalBytes: int64(usage.Total),
			UsedBytes:  int64(usage.Used),
		}

		if io, ok := matchIOCounter(ioCounters, p.Device); ok {
			key := p.Device
			prev, hadPrev := c.prevDiskIO[key]
			if hadPrev {
				elapsed := now.Sub(prev.at).Seconds()
				if elapsed > 0 {
					sample.ReadBytesSec = float64(io.ReadBytes-prev.readBytes) / elapsed
					sample.WriteBytesSec = float64(io.WriteBytes-prev.writeBytes) / elapsed
				}
			}
			c.prevDiskIO[key] = diskIOState{readBytes: io.ReadBytes, writeBytes: io.WriteBytes, at: now}
		}

		samples = append(samples, sample)
	}
	return samples
}

// matchIOCounter maps a partition's device path (e.g. "/dev/sda1") to a
// gopsutil IOCounters entry (keyed by bare device name, e.g. "sda"). Exact
// keys rarely match across platforms, so this falls back to a prefix match.
func matchIOCounter(counters map[string]disk.IOCountersStat, device string) (disk.IOCountersStat, bool) {
	base := strings.TrimPrefix(filepath.Base(device), "/")
	if io, ok := counters[base]; ok {
		return io, true
	}
	for name, io := range counters {
		if strings.HasPrefix(base, name) || strings.HasPrefix(name, base) {
			return io, true
		}
	}
	return disk.IOCountersStat{}, false
}
