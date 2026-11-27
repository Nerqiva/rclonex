//go:build !plan9

package extract

import "testing"

func requireLib7zip(t *testing.T) {
	t.Helper()
	if !lib7zipAvailable() {
		t.Skip("7-Zip engine unavailable in this build")
	}
}
