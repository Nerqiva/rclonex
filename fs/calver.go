package fs

import (
	"strings"

	"github.com/coreos/go-semver/semver"
)

// Semver returns version as a semantic version, accepting both upstream
// rclone version strings and the four-component daily CalVer used by rclonex.
// The per-day counter of a daily CalVer becomes a pre-release identifier so
// the version is representable as semver.
func Semver(version string) (*semver.Version, error) {
	parts := strings.SplitN(version, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) == 4 {
		preRelease := "alpha"
		if len(parts) == 2 && parts[1] != "" {
			preRelease = parts[1]
		}
		version = strings.Join(numbers[:3], ".") + "-" + preRelease + "." + numbers[3]
	}
	return semver.NewVersion(strings.TrimPrefix(version, "v"))
}
