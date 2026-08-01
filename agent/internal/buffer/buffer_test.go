package buffer

import (
	"reflect"
	"testing"

	"sentinel/agent/internal/models"
)

// notBuffered lists IngestPayload fields that deliberately are not
// accumulated. Each needs a reason, because the default must be that a field
// added to the payload is buffered — forgetting one drops data silently.
//
//   - DiscoveredLLM is the agent's current view of which endpoints exist, not
//     a time series. It is attached fresh at push time. Accumulating it would
//     resend the same endpoints once per buffered tick after an outage.
var notBuffered = map[string]string{
	"DiscoveredLLM": "current state, attached at push time, not accumulated",
}

// oneOfEverything returns a payload holding a single sample in every slice
// field of IngestPayload, discovered by reflection rather than listed by
// hand so new metric families are covered automatically.
func oneOfEverything(t *testing.T) models.IngestPayload {
	t.Helper()
	var p models.IngestPayload
	v := reflect.ValueOf(&p).Elem()
	for i := 0; i < v.NumField(); i++ {
		if f := v.Field(i); f.Kind() == reflect.Slice {
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		}
	}
	return p
}

// Add has to enumerate every payload field by hand, so it is easy to extend
// IngestPayload and forget one — which drops those samples silently, with no
// error and nothing in the logs. That happened with the LLM family: the
// collector scraped correctly and every sample was discarded here.
func TestAddCarriesEveryPayloadField(t *testing.T) {
	b := New()
	b.Add(oneOfEverything(t))

	got := reflect.ValueOf(b.Snapshot())
	for i := 0; i < got.NumField(); i++ {
		field := got.Field(i)
		if field.Kind() != reflect.Slice {
			continue
		}
		name := got.Type().Field(i).Name
		if _, exempt := notBuffered[name]; exempt {
			continue
		}
		if field.Len() == 0 {
			t.Errorf("Buffer.Add dropped IngestPayload.%s — add it to Add(), or document it in notBuffered", name)
		}
	}
}

// A payload carrying only one family must still count as non-empty, or the
// push is skipped and those samples are lost.
func TestIsEmptyConsidersEveryPayloadField(t *testing.T) {
	full := oneOfEverything(t)
	v := reflect.ValueOf(full)

	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() != reflect.Slice {
			continue
		}
		name := v.Type().Field(i).Name
		if _, exempt := notBuffered[name]; exempt {
			continue
		}

		// Build a payload populated in exactly one field.
		var only models.IngestPayload
		ov := reflect.ValueOf(&only).Elem()
		ov.Field(i).Set(v.Field(i))

		b := New()
		b.Add(only)
		if b.IsEmpty() {
			t.Errorf("IsEmpty() = true with only %s populated — those samples would never be pushed", name)
		}
	}
}

func TestAddBoundsMemoryDuringOutage(t *testing.T) {
	b := New()
	for i := 0; i < maxSamplesPerMetric+50; i++ {
		b.Add(models.IngestPayload{CPU: []models.CPUSample{{UsagePct: float64(i)}}})
	}

	got := b.Snapshot().CPU
	if len(got) != maxSamplesPerMetric {
		t.Fatalf("buffered %d CPU samples, want cap of %d", len(got), maxSamplesPerMetric)
	}
	// The newest sample must survive; the oldest is the one dropped.
	if want := float64(maxSamplesPerMetric + 49); got[len(got)-1].UsagePct != want {
		t.Errorf("newest sample = %v, want %v — the cap dropped the wrong end", got[len(got)-1].UsagePct, want)
	}
}

func TestClearResetsBuffer(t *testing.T) {
	b := New()
	b.Add(oneOfEverything(t))
	b.Clear()

	if !b.IsEmpty() {
		t.Error("IsEmpty() = false after Clear()")
	}
}
