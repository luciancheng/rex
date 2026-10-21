package watch

import "time"

const (
	lockWait = 500 * time.Millisecond
	lockPoll = 10 * time.Millisecond
)
