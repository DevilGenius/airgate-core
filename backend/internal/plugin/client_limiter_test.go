package plugin

import "testing"

func TestClientRPMGenerationPreservesAllActiveKeysAndDropsOldWindow(t *testing.T) {
	l := newClientLimiter()
	for key := 1; key <= 5000; key++ {
		if !l.allowRPMAt(key, 1, 0, false, 10) {
			t.Fatal("first request rejected")
		}
	}
	for key := 1; key <= 5000; key++ {
		if l.allowRPMAt(key, 1, 0, false, 10) {
			t.Fatal("active counter was evicted")
		}
	}
	if !l.allowRPMAt(1, 1, 0, false, 11) || len(l.keyRPM) != 1 {
		t.Fatal("new minute retained old generation")
	}
}

func TestClientLimiterRPMDimensions(t *testing.T) {
	limiter := newClientLimiter()
	if !limiter.allowRPM(7, 2, 1, true) {
		t.Fatal("first non-Responses request should pass")
	}
	if limiter.allowRPM(7, 2, 1, true) {
		t.Fatal("second non-Responses request should hit non-Responses RPM")
	}
	if !limiter.allowRPM(7, 2, 1, false) {
		t.Fatal("Responses request should still pass under total RPM")
	}
	if limiter.allowRPM(7, 2, 1, false) {
		t.Fatal("third total request should hit total RPM")
	}
}

func TestClientLimiterConcurrencyDimensions(t *testing.T) {
	limiter := newClientLimiter()
	release, userLimited, keyLimited := limiter.acquire(1, 2, 1, 1)
	if release == nil || userLimited || keyLimited {
		t.Fatalf("first acquire = release:%v user:%v key:%v", release != nil, userLimited, keyLimited)
	}
	if next, userLimited, keyLimited := limiter.acquire(1, 2, 1, 1); next != nil || !userLimited || keyLimited {
		t.Fatalf("user limit = release:%v user:%v key:%v", next != nil, userLimited, keyLimited)
	}
	release()
	release, userLimited, keyLimited = limiter.acquire(1, 2, 1, 1)
	if release == nil || userLimited || keyLimited {
		t.Fatalf("acquire after release = release:%v user:%v key:%v", release != nil, userLimited, keyLimited)
	}
	release()
}

func TestClientLimiterZeroConfigDoesNotCreateState(t *testing.T) {
	limiter := newClientLimiter()
	if !limiter.allowRPM(7, 0, 0, false) {
		t.Fatal("zero RPM config should pass")
	}
	if release, userLimited, keyLimited := limiter.acquire(1, 2, 0, 0); release == nil || userLimited || keyLimited {
		t.Fatalf("zero concurrency config = release:%v user:%v key:%v", release != nil, userLimited, keyLimited)
	}
	if len(limiter.keyRPM) != 0 || len(limiter.userConcurrency) != 0 || len(limiter.keyConcurrency) != 0 {
		t.Fatalf("zero config allocated limiter state: %+v", limiter)
	}
}
