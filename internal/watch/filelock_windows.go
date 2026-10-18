//go:build windows

package watch

func withFeedLock(path string, fn func() error) error {
	return fn()
}
