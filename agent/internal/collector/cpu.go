package collector

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"

	"sentinel/agent/internal/models"
)

func (c *Collector) collectCPU(now time.Time) (models.CPUSample, error) {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return models.CPUSample{}, err
	}

	sample := models.CPUSample{Time: now, UsagePct: percents[0]}

	// load averages aren't meaningful on Windows; gopsutil returns an error
	// there, so we just omit them rather than failing the whole sample.
	if avg, err := load.Avg(); err == nil {
		l1, l5, l15 := avg.Load1, avg.Load5, avg.Load15
		sample.Load1, sample.Load5, sample.Load15 = &l1, &l5, &l15
	}

	return sample, nil
}
