package firmware

import (
	"os"
	"syscall"
	"time"
)

// atime is when an image was last served: Cached touches it.
func atime(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atim.Sec, st.Atim.Nsec)
	}
	return info.ModTime()
}
