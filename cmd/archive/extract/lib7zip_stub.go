//go:build !cgo || plan9

package extract

import "fmt"

// extractWithLib7zip is unavailable when cgo is disabled or on plan9.
func extractWithLib7zip(args lib7zipArgs) error {
	return fmt.Errorf("7-Zip engine unavailable: not compiled into this build")
}

// lib7zipAvailable reports whether the engine can be loaded.
func lib7zipAvailable() bool { return false }
