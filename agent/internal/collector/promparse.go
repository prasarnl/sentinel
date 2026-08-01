package collector

import (
	"strconv"
	"strings"
)

// promSample is one line of the Prometheus text exposition format: a metric
// name, its labels, and a value.
type promSample struct {
	Labels map[string]string
	Value  float64
}

// promMetrics holds a parsed /metrics response, keyed by metric name. A name
// maps to several samples when the exporter labels a metric by dimension
// (vLLM, for instance, tags everything with model_name).
type promMetrics map[string][]promSample

// parseProm parses the Prometheus text exposition format. It deliberately
// implements only the subset exporters actually emit — `name{k="v",...} value`
// with # comment lines — rather than pulling in prometheus/common/expfmt,
// which would drag a large dependency tree into an agent binary that today
// has four dependencies total.
//
// Malformed lines are skipped rather than failing the whole scrape: a single
// unparseable metric shouldn't cost us every other metric on the endpoint.
func parseProm(body string) promMetrics {
	metrics := promMetrics{}

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, labels, rest := splitMetricLine(line)
		if name == "" {
			continue
		}

		// The value is the first field after the name/labels; a trailing
		// timestamp, if present, is ignored.
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue // includes NaN-free garbage; Prometheus "NaN" parses fine
		}

		metrics[name] = append(metrics[name], promSample{Labels: labels, Value: value})
	}

	return metrics
}

// splitMetricLine breaks `name{a="1",b="2"} 3.4` into its name, label map, and
// the remainder holding the value. Lines without labels are handled too.
func splitMetricLine(line string) (name string, labels map[string]string, rest string) {
	open := strings.IndexByte(line, '{')
	if open < 0 {
		idx := strings.IndexAny(line, " \t")
		if idx < 0 {
			return "", nil, ""
		}
		return line[:idx], nil, line[idx+1:]
	}

	close := strings.LastIndexByte(line, '}')
	if close < open {
		return "", nil, ""
	}
	return line[:open], parseLabels(line[open+1 : close]), line[close+1:]
}

// parseLabels parses `a="1",b="2"` into a map. Values are taken verbatim
// between quotes; escape sequences are rare in the label values we read
// (model names, device ids) and are left as-is.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}

	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(strings.TrimPrefix(s[:eq], ","))

		rest := strings.TrimSpace(s[eq+1:])
		if !strings.HasPrefix(rest, `"`) {
			break
		}
		rest = rest[1:]
		endQuote := strings.IndexByte(rest, '"')
		if endQuote < 0 {
			break
		}
		labels[key] = rest[:endQuote]

		s = rest[endQuote+1:]
		s = strings.TrimPrefix(strings.TrimSpace(s), ",")
	}

	if len(labels) == 0 {
		return nil
	}
	return labels
}

// value returns the first sample's value for a metric name. Metrics we read
// are single-series per endpoint (one model is served at a time), so taking
// the first sample is sufficient and avoids forcing every caller to deal with
// label matching.
func (m promMetrics) value(name string) (float64, bool) {
	samples := m[name]
	if len(samples) == 0 {
		return 0, false
	}
	return samples[0].Value, true
}

// ptr returns a pointer to the metric's value, or nil when the endpoint
// doesn't expose it. Absent metrics stay nil all the way to the dashboard so
// the UI can hide them rather than render a misleading zero.
func (m promMetrics) ptr(name string) *float64 {
	v, ok := m.value(name)
	if !ok {
		return nil
	}
	return &v
}

// hasPrefix reports whether any metric name starts with the given prefix,
// which is how a runtime is identified ("vllm:" vs "llamacpp:").
func (m promMetrics) hasPrefix(prefix string) bool {
	for name := range m {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// label returns the value of a label on a metric's first sample, e.g. the
// model_name vLLM attaches to its series.
func (m promMetrics) label(name, key string) string {
	samples := m[name]
	if len(samples) == 0 {
		return ""
	}
	return samples[0].Labels[key]
}
