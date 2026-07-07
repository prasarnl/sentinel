package buffer

import "sentinel/agent/internal/models"

// maxSamplesPerMetric bounds memory use during an outage; once full, the
// oldest sample in that category is dropped to make room for the newest.
const maxSamplesPerMetric = 500

// Buffer accumulates samples that couldn't be pushed yet (e.g. the server
// was unreachable) so they're retried on the next tick instead of lost.
type Buffer struct {
	payload models.IngestPayload
}

func New() *Buffer {
	return &Buffer{}
}

func (b *Buffer) Add(p models.IngestPayload) {
	b.payload.CPU = appendBounded(b.payload.CPU, p.CPU...)
	b.payload.Mem = appendBounded(b.payload.Mem, p.Mem...)
	b.payload.Disk = appendBounded(b.payload.Disk, p.Disk...)
	b.payload.GPU = appendBounded(b.payload.GPU, p.GPU...)
}

func (b *Buffer) IsEmpty() bool {
	return len(b.payload.CPU) == 0 && len(b.payload.Mem) == 0 && len(b.payload.Disk) == 0 && len(b.payload.GPU) == 0
}

func (b *Buffer) Snapshot() models.IngestPayload {
	return b.payload
}

func (b *Buffer) Clear() {
	b.payload = models.IngestPayload{}
}

func appendBounded[T any](slice []T, items ...T) []T {
	slice = append(slice, items...)
	if len(slice) > maxSamplesPerMetric {
		slice = slice[len(slice)-maxSamplesPerMetric:]
	}
	return slice
}
