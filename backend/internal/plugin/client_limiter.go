package plugin

import (
	"sync"
	"time"
)

// clientLimiter stores API Key/user concurrency and API Key RPM state in the
// Core process. It deliberately avoids Redis on the request hot path.
type clientLimiter struct {
	concurrencyMu sync.Mutex
	rpmMu         sync.Mutex

	userConcurrency map[int]int
	keyConcurrency  map[int]int
	keyRPM          map[int]clientRPMWindow
}

type clientRPMWindow struct {
	minute       int64
	total        int
	nonResponses int
}

func newClientLimiter() *clientLimiter {
	return &clientLimiter{
		userConcurrency: make(map[int]int),
		keyConcurrency:  make(map[int]int),
		keyRPM:          make(map[int]clientRPMWindow),
	}
}

func (l *clientLimiter) acquire(userID, keyID, userMax, keyMax int) (release func(), userLimited, keyLimited bool) {
	if l == nil || (userMax <= 0 && keyMax <= 0) {
		return func() {}, false, false
	}

	l.concurrencyMu.Lock()
	defer l.concurrencyMu.Unlock()
	if userMax > 0 && l.userConcurrency[userID] >= userMax {
		return nil, true, false
	}
	if keyMax > 0 && l.keyConcurrency[keyID] >= keyMax {
		return nil, false, true
	}
	if userMax > 0 {
		l.userConcurrency[userID]++
	}
	if keyMax > 0 {
		l.keyConcurrency[keyID]++
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			l.concurrencyMu.Lock()
			if userMax > 0 {
				decrementClientCount(l.userConcurrency, userID)
			}
			if keyMax > 0 {
				decrementClientCount(l.keyConcurrency, keyID)
			}
			l.concurrencyMu.Unlock()
		})
	}, false, false
}

func (l *clientLimiter) allowRPM(keyID, totalMax, nonResponsesMax int, countNonResponses bool) bool {
	if l == nil || keyID <= 0 || (totalMax <= 0 && (!countNonResponses || nonResponsesMax <= 0)) {
		return true
	}

	minute := time.Now().Unix() / 60
	l.rpmMu.Lock()
	defer l.rpmMu.Unlock()

	window := l.keyRPM[keyID]
	if window.minute != minute {
		window = clientRPMWindow{minute: minute}
	}
	if totalMax > 0 && window.total >= totalMax {
		return false
	}
	if countNonResponses && nonResponsesMax > 0 && window.nonResponses >= nonResponsesMax {
		return false
	}
	if totalMax > 0 {
		window.total++
	}
	if countNonResponses && nonResponsesMax > 0 {
		window.nonResponses++
	}
	l.keyRPM[keyID] = window
	if len(l.keyRPM) > 4096 {
		for id, item := range l.keyRPM {
			if item.minute != minute {
				delete(l.keyRPM, id)
			}
		}
	}
	return true
}

func decrementClientCount(counts map[int]int, id int) {
	if current := counts[id] - 1; current > 0 {
		counts[id] = current
	} else {
		delete(counts, id)
	}
}
