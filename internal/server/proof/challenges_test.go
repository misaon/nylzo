package proof

import (
	"math"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core/signing"
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
		if accepted := see(challenges, challenge, now); accepted != testCase.accepted {
			t.Fatalf("a challenge issued %v from now: accepted %v", testCase.issued.Sub(now), accepted)
		}
	}
	challenge, _, _ := challenges.Issue(now)
	forged := challenge
	forged[len(forged)-1] ^= 1
	if see(challenges, forged, now) {
		t.Fatal("a challenge with a wrong tag was accepted")
	}
	if !see(challenges, challenge, now) {
		t.Fatal("a forged copy used up the genuine challenge")
	}
	if see(challenges, challenge, now) {
		t.Fatal("the process accepted a challenge it had seen")
	}
	early, _, _ := challenges.Issue(now.Add(6 * time.Second))
	see(challenges, early, now)
	if see(challenges, early, now.Add(time.Second)) {
		t.Fatal("a challenge refused for its window was accepted once the clock caught up")
	}
}

func TestAProcessAcceptsOnlyChallengesItIssuedAndStillHolds(t *testing.T) {
	t.Parallel()
	now := time.Now()
	earlier := NewChallenges(testRing(t), MinCapacity)
	challenges := NewChallenges(testRing(t), MinCapacity)
	fromEarlier, _, _ := earlier.Issue(now)
	if see(challenges, fromEarlier, now) {
		t.Fatal("a process accepted a challenge that another process issued")
	}
	behind := now.Add(-10 * time.Second)
	own, _, _ := challenges.Issue(behind)
	if !see(challenges, own, behind) {
		t.Fatal("a process refused its own challenge after its clock stepped back")
	}
	expired, _, _ := challenges.Issue(now)
	ahead, _, _ := challenges.Issue(now.Add(time.Hour))
	live, _, _ := challenges.Issue(now.Add(challengeTTL))
	challenges.Purge(now.Add(challengeTTL + clockSkew + time.Second))
	if see(challenges, expired, now) {
		t.Fatal("a challenge the purge dropped was accepted after the clock stepped back")
	}
	if see(challenges, ahead, now.Add(time.Hour)) {
		t.Fatal("a challenge issued by a clock that later stepped back was kept by the purge")
	}
	if !see(challenges, live, now.Add(challengeTTL)) {
		t.Fatal("the purge dropped a challenge whose window was open")
	}
}

// A handshake's challenge stays open while its client stretches the passphrase, and the purge keeps it (spec 6.4).
func TestAHandshakeChallengeOutlivesAnOrdinaryOne(t *testing.T) {
	t.Parallel()
	now := time.Now()
	challenges := NewChallenges(testRing(t), MinCapacity)
	ordinary, _, _ := challenges.Issue(now)
	login, _, _ := challenges.IssueLogin(now)
	later := now.Add(challengeTTL + 30*time.Second)
	challenges.Purge(later)
	if see(challenges, ordinary, later) {
		t.Fatal("an ordinary challenge was accepted after its minute")
	}
	if !see(challenges, login, later) {
		t.Fatal("a handshake's challenge was refused while its client could still be stretching")
	}
	forgotten, _, _ := challenges.IssueLogin(now)
	challenges.Forget(forgotten)
	if see(challenges, forgotten, now) {
		t.Fatal("a challenge its refused handshake gave back was accepted")
	}
	early, _, _ := challenges.IssueLogin(now.Add(clockSkew + time.Second))
	if see(challenges, early, now) {
		t.Fatal("a handshake's challenge from a clock ahead was accepted")
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
	if !see(challenges, used, later) {
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
		{20, 1000, MinCapacity},
		{2000, 2000, 2 * (2000*966 + 2000)},
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

func see(challenges *Challenges, challenge [signing.ChallengeLen]byte, now time.Time) bool {
	accepted, _ := challenges.Take(challenge, now)
	return accepted
}

func filledLogin() *Login {
	login := &Login{}
	for i := range login.Server.ExpectedClientMAC {
		login.Server.ExpectedClientMAC[i] = 0xff
	}
	return login
}

func wiped(login *Login) bool {
	return login.Server.ExpectedClientMAC == [len(login.Server.ExpectedClientMAC)]byte{}
}

func TestEveryWayAChallengeEndsWipesItsLogin(t *testing.T) {
	t.Parallel()
	now := time.Now()
	challenges := NewChallenges(testRing(t), MinCapacity)
	attached := func() ([signing.ChallengeLen]byte, *Login) {
		challenge, _, _ := challenges.IssueLogin(now)
		login := filledLogin()
		if !challenges.Attach(challenge, login) {
			t.Fatal("a login did not attach to its challenge")
		}
		return challenge, login
	}
	late, lateLogin := attached()
	if accepted, handed := challenges.Take(late, now.Add(time.Hour)); accepted || handed != nil || !wiped(lateLogin) {
		t.Error("a challenge taken after its window kept or handed over its login")
	}
	forgotten, forgottenLogin := attached()
	challenges.Forget(forgotten)
	if !wiped(forgottenLogin) {
		t.Error("a forgotten challenge kept its login")
	}
	_, purgedLogin := attached()
	challenges.Purge(now.Add(time.Hour))
	if !wiped(purgedLogin) {
		t.Error("a purged challenge kept its login")
	}
}

func TestTimesOutsideTheProtocolFieldAreZero(t *testing.T) {
	t.Parallel()
	for _, moment := range []time.Time{time.Unix(-1, 0), time.Unix(math.MaxUint32+1, 0)} {
		if seconds := unixSeconds(moment); seconds != 0 {
			t.Errorf("%v: %d", moment, seconds)
		}
	}
}
