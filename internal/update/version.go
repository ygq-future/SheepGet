package update

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion parses a version string in "X.Y.Z" or "vX.Y.Z" format into numeric segments.
func ParseVersion(v string) (major, minor, patch int, err error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid semver: %q (expected X.Y.Z)", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %w", err)
	}
	// Support optional build/prerelease suffix by splitting patch on '-' or '+'
	patchStr := parts[2]
	if idx := strings.IndexAny(patchStr, "-+"); idx != -1 {
		patchStr = patchStr[:idx]
	}
	patch, err = strconv.Atoi(patchStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version: %w", err)
	}
	return major, minor, patch, nil
}

// CompareVersions compares two semver strings.
// Returns:
//   - 1 if v1 > v2
//   - -1 if v1 < v2
//   - 0 if v1 == v2
func CompareVersions(v1, v2 string) (int, error) {
	maj1, min1, pat1, err1 := ParseVersion(v1)
	if err1 != nil {
		return 0, err1
	}
	maj2, min2, pat2, err2 := ParseVersion(v2)
	if err2 != nil {
		return 0, err2
	}

	if maj1 != maj2 {
		if maj1 > maj2 {
			return 1, nil
		}
		return -1, nil
	}
	if min1 != min2 {
		if min1 > min2 {
			return 1, nil
		}
		return -1, nil
	}
	if pat1 != pat2 {
		if pat1 > pat2 {
			return 1, nil
		}
		return -1, nil
	}
	return 0, nil
}
