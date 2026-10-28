//go:build unix

package plan

import (
	"os"
	"syscall"
)

// links is how many names the file has: above 1, it has hard links.
func links(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
