package adminapi

import (
	"testing"
	"time"
)

// A session ends fifteen minutes after its last request and twelve hours after sign-in however busy it is; the
// fifth session ends the oldest; and a sign-in challenge is bound to its session key, runs out and opens one
// session.
func TestSessionsEndWhenIdleOrOldAndChallengesWorkOnce(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	held := newSessions()
	key, other := sessionKey{1}, sessionKey{2}
	challenge := held.signInChallenge(key, now)
	if !held.signInOpen(challenge, key, now) {
		t.Fatal("a fresh challenge is not open for its key")
	}
	if held.signInOpen(challenge, other, now) {
		t.Fatal("a challenge is open for another session key")
	}
	if held.signInOpen(challenge, key, now.Add(ceremonyLife)) {
		t.Fatal("a challenge is open after it ran out")
	}
	if _, first := held.open(key, []byte("passkey"), challenge, time.Time{}, now); !first {
		t.Fatal("the challenge opened no session")
	}
	if _, again := held.open(other, []byte("passkey"), challenge, time.Time{}, now); again ||
		held.signInOpen(challenge, key, now) {
		t.Fatal("a challenge opened a second session")
	}
	if _, live := held.touch(key, now.Add(sessionIdle-time.Second)); !live {
		t.Fatal("a session ended before it was idle for fifteen minutes")
	}
	if _, live := held.touch(key, now.Add(2*sessionIdle)); live {
		t.Fatal("an idle session lived on")
	}
	busy := sessionKey{3}
	held.open(busy, []byte("passkey"), held.signInChallenge(busy, now), time.Time{}, now)
	for at := now; at.Before(now.Add(sessionLife)); at = at.Add(10 * time.Minute) {
		if _, live := held.touch(busy, at); !live {
			t.Fatalf("a busy session ended after %s", at.Sub(now))
		}
	}
	if _, live := held.touch(busy, now.Add(sessionLife)); live {
		t.Fatal("a session outlived twelve hours")
	}
	keys := make([]sessionKey, maxSessions+1)
	for index := range keys {
		keys[index][0] = byte(10 + index)
		opened := now.Add(time.Duration(index) * time.Second)
		held.open(keys[index], []byte("passkey"), held.signInChallenge(keys[index], opened), time.Time{}, opened)
	}
	if _, live := held.touch(keys[0], now.Add(time.Minute)); live {
		t.Fatal("the fifth session did not end the oldest")
	}
	for _, kept := range keys[1:] {
		if _, live := held.touch(kept, now.Add(time.Minute)); !live {
			t.Fatal("a session other than the oldest ended")
		}
	}
	held.keep(func(string) bool { return false }, now.Add(time.Minute))
	if _, live := held.touch(keys[1], now.Add(time.Minute)); live {
		t.Fatal("a session outlived its passkey")
	}
}
