package fs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSemver(t *testing.T) {
	for _, test := range []struct {
		version string
		want    string
	}{
		{"v2026.11.27.1-alpha", "2026.11.27-alpha.1"},
		{"v2026.11.27.1-alpha-DEV", "2026.11.27-alpha-DEV.1"},
		{"v2026.11.0-alpha.5", "2026.11.0-alpha.5"},
		{"v1.76.0", "1.76.0"},
	} {
		v, err := Semver(test.version)
		assert.NoError(t, err, test.version)
		if err == nil {
			assert.Equal(t, test.want, v.String(), test.version)
		}
	}
}

func TestSemverOrdering(t *testing.T) {
	a, err := Semver("v2026.11.27.1-alpha")
	assert.NoError(t, err)
	b, err := Semver("v2026.11.27.2-alpha")
	assert.NoError(t, err)
	assert.True(t, a.LessThan(*b))
}
