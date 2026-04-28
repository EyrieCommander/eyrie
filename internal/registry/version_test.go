package registry

import "testing"

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"zeroclaw 0.7.2", "0.7.2"},
		{"v1.2.3-beta", "1.2.3"},
		{"OpenClaw CLI version 2.1.0 (node 22.1)", "2.1.0"},
		{"picoclaw/0.3.0 (go1.21)", "0.3.0"},
		{"0.7.3", "0.7.3"},
		{"v10.20.30", "10.20.30"},
		{"no version here", ""},
		{"", ""},
		{"just numbers 42", ""},
		{"partial 1.2", ""},
	}
	for _, tt := range tests {
		got := ExtractVersion(tt.input)
		if got != tt.want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.7.2", "0.7.0", 1},
		{"0.7.0", "0.7.2", -1},
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.9.9", 1},
		{"0.1.0", "0.2.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"", "1.0.0", 0},
		{"1.0.0", "", 0},
		{"", "", 0},
		{"garbage", "1.0.0", 0},
		{"1.0.0", "not.a.version", 0},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestVersionStatus(t *testing.T) {
	tests := []struct {
		installed, min, latest string
		want                   string
	}{
		// No data
		{"", "0.7.0", "0.8.0", ""},
		{"0.7.0", "", "", ""},
		// Outdated (below min)
		{"0.6.0", "0.7.0", "0.8.0", "outdated"},
		{"0.1.7", "0.7.0", "0.7.3", "outdated"},
		// Update available (meets min, below latest)
		{"0.7.0", "0.7.0", "0.8.0", "update_available"},
		{"0.7.2", "0.7.0", "0.7.3", "update_available"},
		// Current (at or above latest)
		{"0.8.0", "0.7.0", "0.8.0", "current"},
		{"1.0.0", "0.7.0", "0.8.0", "current"},
		// Only min_version set (no latest)
		{"0.6.0", "0.7.0", "", "outdated"},
		{"0.7.0", "0.7.0", "", "current"},
		{"0.8.0", "0.7.0", "", "current"},
		// Only latest_version set (no min)
		{"0.6.0", "", "0.8.0", "update_available"},
		{"0.8.0", "", "0.8.0", "current"},
	}
	for _, tt := range tests {
		got := VersionStatus(tt.installed, tt.min, tt.latest)
		if got != tt.want {
			t.Errorf("VersionStatus(%q, %q, %q) = %q, want %q",
				tt.installed, tt.min, tt.latest, got, tt.want)
		}
	}
}
