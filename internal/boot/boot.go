// Package boot holds when the process started. It imports only time, so
// Go's init order (by import path, each package once its imports are
// done) sets it before the packages that take a while to set up, such as
// internal/mascot's sprites, and a prompt hook's deadline (docs/ux.md §7a)
// counts their time too.
package boot

import "time"

// At is when the process started, near enough.
var At = time.Now()
