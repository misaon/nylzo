package proof

import (
	"math"
	"testing"
	"time"
)

func TestChallengesAreUsedOnceWithinTheirWindow(t *testing.T) {
	t.Parallel()
	now := time.Now()
	challenges := NewChallenges(testRing(t), MinCapacity)
	for _, testCase := range []struct {
		issued   time.Time
		accepted bool
	}{
		{now, true},
		{now.Add(5 * time.Second), true},
		{now.Add(6 * time.Second), false},
		{now.Add(-challengeTTL - time.Second), false},
	} {
		challenge, _, _ := challenges.Issue(testCase.issued)
		if accepted := challenges.See(challenge, now); accepted != testCase.accepted {
			t.Fatalf("a challenge issued %v from now: accepted %v", testCase.issued.Sub(now), accepted)
		}
	}
	challenge, _, _ := challenges.Issue(now)
	forged := challenge
	forged[len(forged)-1] ^= 1
	if challenges.See(forged, now) {
		t.Fatal("a challenge with a wrong tag was accepted")
	}
	if !challenges.See(challenge, now) {
		t.Fatal("a forged copy used up the genuine challenge")
	}
	if challenges.See(challenge, now) {
		t.Fatal("the process accepted a challenge it had seen")
	}
	early, _, _ := challenges.Issue(now.Add(6 * time.Second))
	challenges.See(early, now)
	if challenges.See(early, now.Add(time.Second)) {
		t.Fatal("a challenge refused for its window was accepted once the clock caught up")
	}
}

func TestAProcessAcceptsOnlyChallengesItIssuedAndStillHolds(t *testing.T) {
	t.Parallel()
	now := time.Now()
	earlier := NewChallenges(testRing(t), MinCapacity)
	challenges := NewChallenges(testRing(t), MinCapacity)
	fromEarlier, _, _ := earlier.Issue(now)
	if challenges.See(fromEarlier, now) {
		t.Fatal("a process accepted a challenge that another process issued")
	}
	behind := now.Add(-10 * time.Second)
	own, _, _ := challenges.Issue(behind)
	if !challenges.See(own, behind) {
		t.Fatal("a process refused its own challenge after its clock stepped back")
	}
	expired, _, _ := challenges.Issue(now)
	ahead, _, _ := challenges.Issue(now.Add(time.Hour))
	live, _, _ := challenges.Issue(now.Add(challengeTTL))
	challenges.Purge(now.Add(challengeTTL + clockSkew + time.Second))
	if challenges.See(expired, now) {
		t.Fatal("a challenge the purge dropped was accepted after the clock stepped back")
	}
	if challenges.See(ahead, now.Add(time.Hour)) {
		t.Fatal("a challenge issued by a clock that later stepped back was kept by the purge")
	}
	if !challenges.See(live, now.Add(challengeTTL)) {
		t.Fatal("the purge dropped a challenge whose window was open")
	}
}

func TestAFullTableMakesRoomForNewChallenges(t *testing.T) {
	t.Parallel()
	now := time.Now()
	challenges := NewChallenges(testRing(t), 2)
	challenges.Issue(now)
	challenges.Issue(now)
	if _, _, issued := challenges.Issue(now); issued {
		t.Fatal("a process issued more challenges than it may hold")
	}
	later := now.Add(challengeTTL + clockSkew + time.Second)
	if _, _, issued := challenges.Issue(later); !issued {
		t.Fatal("a full table of expired challenges refused a new one")
	}
	used, _, _ := challenges.Issue(later)
	if !challenges.See(used, later) {
		t.Fatal("a held challenge was refused")
	}
	if _, _, issued := challenges.Issue(later); !issued {
		t.Fatal("a used challenge kept its place in the table")
	}
}

func TestCapacityHoldsTwiceWhatTheInstanceBucketCanIssue(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		perSecond, burst float64
		capacity         int
	}{
		{500, 1000, MinCapacity},
		{2000, 2000, 2 * (2000*126 + 2000)},
		{1e300, 1e300, math.MaxInt32},
	} {
		if capacity := Capacity(testCase.perSecond, testCase.burst); capacity != testCase.capacity {
			t.Errorf(
				"rate %g burst %g: capacity %d, want %d",
				testCase.perSecond,
				testCase.burst,
				capacity,
				testCase.capacity,
			)
		}
	}
}
