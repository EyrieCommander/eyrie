package registry

import (
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

// ExtractVersion pulls the first semver-ish version (X.Y.Z) from arbitrary
// --version output. Returns "" if no version pattern is found.
//
//	"zeroclaw 0.7.2"            → "0.7.2"
//	"v1.2.3-beta"               → "1.2.3"
//	"OpenClaw CLI version 2.1.0" → "2.1.0"
func ExtractVersion(raw string) string {
	m := semverRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// CompareVersions compares two dotted version strings (e.g. "1.2.3").
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Returns 0 if either string is empty or unparseable (unknown = no opinion).
func CompareVersions(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	ap := parseVersion(a)
	bp := parseVersion(b)
	if ap == nil || bp == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

// VersionStatus determines the version state given the installed version
// and the registry's min/latest constraints. Returns:
//
//	""                 — no version data available
//	"outdated"         — below min_version (may have compatibility issues)
//	"update_available" — meets min_version but below latest_version
//	"current"          — at or above latest_version
func VersionStatus(installed, minVersion, latestVersion string) string {
	if installed == "" {
		return ""
	}
	if minVersion == "" && latestVersion == "" {
		return ""
	}
	if minVersion != "" && CompareVersions(installed, minVersion) < 0 {
		return "outdated"
	}
	if latestVersion != "" && CompareVersions(installed, latestVersion) < 0 {
		return "update_available"
	}
	return "current"
}

// ComputeVersionStatus extracts a semver from raw --version output and
// compares it against a framework's min/latest constraints. Convenience
// wrapper used by API handlers to avoid duplicating the extract+compare.
func ComputeVersionStatus(rawVersion string, fw Framework) string {
	if rawVersion == "" {
		return ""
	}
	return VersionStatus(ExtractVersion(rawVersion), fw.MinVersion, fw.LatestVersion)
}

// parseVersion splits "1.2.3" into [1, 2, 3]. Returns nil on failure.
func parseVersion(s string) []int {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}
