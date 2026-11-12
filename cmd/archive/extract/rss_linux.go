//go:build linux

package extract

import "syscall"

// maxRSSBytes returns the peak RSS of this process in bytes. On Linux
// ru_maxrss is reported in kibibytes.
func maxRSSBytes() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return int64(ru.Maxrss) * 1024
}
