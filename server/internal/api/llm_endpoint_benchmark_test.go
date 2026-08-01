package api

import "testing"

func TestBenchmarkURLFor(t *testing.T) {
	dgx := "dgx.kanomcakey.com"
	blank := ""

	tests := []struct {
		name     string
		url      string
		hostName *string
		want     string
	}{
		{
			// The case that motivates all of this: an agent scrapes its own
			// host over loopback, so the monitored URL means the Sentinel
			// server itself if used from there.
			name:     "loopback on a host becomes the host name",
			url:      "http://127.0.0.1:8035",
			hostName: &dgx,
			want:     "http://dgx.kanomcakey.com:8035",
		},
		{
			name:     "localhost is treated the same as 127.0.0.1",
			url:      "http://localhost:8027",
			hostName: &dgx,
			want:     "http://dgx.kanomcakey.com:8027",
		},
		{
			// A remote endpoint is already addressed from the server's
			// point of view, so rewriting it would break it.
			name:     "remote endpoint is left alone",
			url:      "http://llm.kanomcakey.com:1234",
			hostName: nil,
			want:     "http://llm.kanomcakey.com:1234",
		},
		{
			// Bound to a routable address on a monitored host: already
			// reachable, so the host name adds nothing.
			name:     "non-loopback on a host is left alone",
			url:      "http://192.168.1.50:8000",
			hostName: &dgx,
			want:     "http://192.168.1.50:8000",
		},
		{
			name:     "a host with no name cannot be substituted in",
			url:      "http://127.0.0.1:8035",
			hostName: &blank,
			want:     "http://127.0.0.1:8035",
		},
		{
			name:     "https and a default port survive",
			url:      "https://127.0.0.1",
			hostName: &dgx,
			want:     "https://dgx.kanomcakey.com",
		},
		{
			name:     "trailing slash is normalized away",
			url:      "http://127.0.0.1:8035/",
			hostName: &dgx,
			want:     "http://dgx.kanomcakey.com:8035",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := benchmarkURLFor(tc.url, tc.hostName); got != tc.want {
				t.Errorf("benchmarkURLFor(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "localhost", "LOCALHOST", "::1", "0.0.0.0", ""} {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"dgx.kanomcakey.com", "192.168.1.50", "10.0.0.1"} {
		if isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = true, want false", h)
		}
	}
}
