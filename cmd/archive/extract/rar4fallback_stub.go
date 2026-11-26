//go:build (!cgo || !unarr) && !plan9

package extract

import (
	"context"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// extractRAR4WithUnarr is the fallback used when unarr is not compiled in.
func extractRAR4WithUnarr(ctx context.Context, srcObj fs.Object, windowSize int64, tr *accounting.Transfer, handleEntry entryHandler) error {
	return errRAR4FallbackUnavailable
}
