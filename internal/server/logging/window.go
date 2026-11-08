package logging

import "time"

// Window is the time whose request counts and events one flush writes. It ends at the start of an hour once it has
// run for an hour, to the minute, so it spans one to two hours and no line dates a request or an event more finely.
type Window struct {
	start time.Time
}

func StartWindow(now time.Time) Window {
	return Window{start: now}
}

// Ended reports whether the window ended by now and then starts the next one. The wall clock aligns the end to the
// hour; the monotonic clock keeps a clock step from ending a window early, and a wall clock that stepped back before
// the start restarts the window, so the step cannot hold the counts back until the clock catches up.
func (w *Window) Ended(now time.Time) bool {
	if now.Round(0).Before(w.start.Round(0)) {
		w.start = now
		return false
	}
	toLastHour := now.Truncate(time.Hour).Sub(w.start.Truncate(time.Minute))
	if toLastHour < time.Hour || now.Sub(w.start) < time.Hour-time.Minute {
		return false
	}
	w.start = now
	return true
}
