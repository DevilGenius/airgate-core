package safego

import "log/slog"

// Go runs fn in a goroutine and prevents a panic from crashing the process.
func Go(name string, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("background_goroutine_panic",
					"name", name,
					"panic", recovered,
				)
			}
		}()
		fn()
	}()
}
