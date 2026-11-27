//go:build !cgo || plan9

package extract

import "fmt"

// extractWithLib7zip is unavailable when cgo is disabled or on plan9.
func extractWithLib7zip(args lib7zipArgs) error {
	return fmt.Errorf("%w: not compiled into this build", errLib7zipUnavailable)
}
