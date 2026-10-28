//go:build !unix

package plan

import "os"

// links is how many names the file has; here it can't be read, so 1.
func links(os.FileInfo) uint64 { return 1 }
