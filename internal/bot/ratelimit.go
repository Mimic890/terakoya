package bot

import (
	"sync"
	"time"
)

// Every user may send a burst of updates, such as an album of ten
// pictures with a caption, and then a few a second. Beyond that, updates
// are dropped: Telegram's limit on what the bot sends is shared by all
// users, and one user tapping as fast as they can must not use it up.
const (
	rateBurst = 20
	rateEvery = time.Second / 3
)

// limiter is a token bucket per user.
type limiter struct {
	burst float64
	every time.Duration // one more token each

	mu    sync.Mutex
	users map[int64]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(burst int, every time.Duration) *limiter {
	return &limiter{burst: float64(burst), every: every, users: map[int64]*bucket{}}
}

// allow takes a token from the user's bucket, if there is one. A nil
// limiter allows everything.
func (l *limiter) allow(id int64, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.users[id]
	if b == nil {
		if len(l.users) >= maxBuckets {
			l.prune(now)
		}
		b = &bucket{tokens: l.burst, at: now}
		l.users[id] = b
	}
	b.tokens = min(l.burst, b.tokens+float64(now.Sub(b.at))/float64(l.every))
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// maxBuckets is how many users are tracked before full buckets, which
// are the same as none, are dropped.
const maxBuckets = 10000

func (l *limiter) prune(now time.Time) {
	full := time.Duration(l.burst * float64(l.every))
	for id, b := range l.users {
		if now.Sub(b.at) >= full {
			delete(l.users, id)
		}
	}
}
