package collector

import (
	"time"

	"github.com/shirou/gopsutil/v3/mem"

	"sentinel/agent/internal/models"
)

func (c *Collector) collectMem(now time.Time) (models.MemSample, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return models.MemSample{}, err
	}

	sample := models.MemSample{
		Time:           now,
		TotalBytes:     int64(vm.Total),
		UsedBytes:      int64(vm.Used),
		AvailableBytes: int64(vm.Available),
	}

	if swap, err := mem.SwapMemory(); err == nil {
		sample.SwapUsedBytes = int64(swap.Used)
		sample.SwapTotalBytes = int64(swap.Total)
	}

	return sample, nil
}
