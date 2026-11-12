//go:build !plan9 && !linux

package extract

// maxRSSBytes returns 0 on platforms where peak RSS is not measured here.
func maxRSSBytes() int64 {
	return 0
}
