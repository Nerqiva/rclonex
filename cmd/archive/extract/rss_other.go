//go:build !plan9 && !linux

package extract

func maxRSSBytes() int64 {
	return 0
}
