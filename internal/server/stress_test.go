package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/sweep"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/server/logging"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	sweepDirEnv  = "NYLZO_TEST_SWEEP_DIR"
	sweepNowEnv  = "NYLZO_TEST_SWEEP_NOW"
	sweptMarker  = "nylzo-test-swept"
	stressInFlow = 32
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(sweepDirEnv); dir != "" {
		os.Exit(sweepProcess(dir, os.Getenv(sweepNowEnv)))
	}
	os.Exit(m.Run())
}

func sweepProcess(dir, at string) int {
	now, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		fmt.Fprintln(os.Stderr, "time:", err)
		return 1
	}
	ctx := context.Background()
	st, err := sqlite.OpenCurrent(ctx, filepath.Join(dir, "nylzo.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		return 1
	}
	defer st.Close()
	blobs, err := disk.New(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "blobs:", err)
		return 1
	}
	if _, err := sweep.Run(ctx, st, blobs, dir, now); !onlyCheckpointBusy(err) {
		fmt.Fprintln(os.Stderr, "sweep:", logging.ErrorText(err))
		return 1
	}
	fmt.Fprintln(os.Stdout, sweptMarker)
	return 0
}

func onlyCheckpointBusy(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, leaf := range joined.Unwrap() {
			if !onlyCheckpointBusy(leaf) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return onlyCheckpointBusy(wrapped)
	}
	return errors.Is(err, sqlite.ErrCheckpointBusy)
}

type endings struct {
	claims   atomic.Int32
	goneAcks atomic.Int32
	burns    atomic.Int32
	cancels  atomic.Int32
}

func TestStressEndingsRaceSweepsInTwoProcesses(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	cfg := h.cfg
	cfg.Rates.ReadPerHour = 1_000_000
	cfg.Rates.CreatePerHour = 1_000_000
	cfg.Rates.CreatePerDay = 1_000_000
	cfg.Rates.CreateBytesPerDay = 1 << 40
	cfg.MaxStreams = config.MaxStreams
	h.reopen(cfg)
	const count = 30
	messages := make([]*message, count)
	got := make([]endings, count)
	for i := range messages {
		messages[i] = newMessage(t, core.KDFNone, "", uint16(1+i%3), 3600, "stress")
		h.mustCreate(messages[i])
	}
	expect := func(what string, code int, allowed ...int) {
		if !slices.Contains(allowed, code) {
			t.Errorf("%s answered %d, want one of %v", what, code, allowed)
		}
	}
	stop := make(chan struct{})
	var background sync.WaitGroup
	background.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := h.srv.SweepOnce(t.Context()); !onlyCheckpointBusy(err) {
				t.Errorf("sweep in the server process: %v", err)
			}
		}
	})
	var overlapping atomic.Int32
	var requestsDone atomic.Bool
	background.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			during := !requestsDone.Load()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			cmd.Env = append(
				os.Environ(),
				sweepDirEnv+"="+h.dir,
				sweepNowEnv+"="+h.clock.Now().Format(time.RFC3339Nano),
			)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), sweptMarker) {
				t.Errorf("sweep-once in a second process: %v %s", err, out)
				return
			}
			if during {
				overlapping.Add(1)
			}
		}
	})
	slots := make(chan struct{}, stressInFlow)
	limit := func(run func()) func() {
		return func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			run()
		}
	}
	var requests sync.WaitGroup
	for i, m := range messages {
		for j := range 4 {
			ip := fmt.Sprintf("10.%d.%d.1", i, j)
			requests.Go(limit(func() {
				resp, _, err := h.tryDo(claimRequest(m, "", ip))
				if err != nil {
					t.Error(err)
					return
				}
				expect("claim", resp.StatusCode, http.StatusOK, http.StatusGone)
				if resp.StatusCode != http.StatusOK {
					return
				}
				got[i].claims.Add(1)
				ack, _, err := h.tryDo(
					request{
						method:  "POST",
						path:    "/api/v1/messages/" + m.id.String() + "/ack",
						ip:      ip,
						token:   m.readToken,
						headers: map[string]string{"X-Nylzo-Lease": resp.Header.Get("X-Nylzo-Lease")},
					},
				)
				if err != nil {
					t.Error(err)
					return
				}
				expect("ack", ack.StatusCode, http.StatusNoContent, http.StatusGone)
				if ack.StatusCode == http.StatusGone {
					got[i].goneAcks.Add(1)
				}
			}))
		}
		if i%2 == 0 {
			requests.Go(limit(func() {
				code := h.statusFrom(
					request{
						method: "POST",
						path:   "/api/v1/messages/" + m.id.String() + "/burn",
						ip:     fmt.Sprintf("10.%d.10.1", i),
						token:  m.readToken,
					},
				)
				expect("burn", code, http.StatusNoContent, http.StatusGone)
				if code == http.StatusNoContent {
					got[i].burns.Add(1)
				}
			}))
		}
		if i%3 == 0 {
			requests.Go(limit(func() {
				code := h.statusFrom(
					request{
						method: "POST",
						path:   "/api/v1/messages/" + m.id.String() + "/cancel",
						ip:     fmt.Sprintf("10.%d.11.1", i),
						token:  m.manageTok,
					},
				)
				expect("cancel", code, http.StatusNoContent, http.StatusGone)
				if code == http.StatusNoContent {
					got[i].cancels.Add(1)
				}
			}))
		}
		requests.Go(limit(func() {
			code := h.statusFrom(
				request{
					method: "GET",
					path:   "/api/v1/messages/" + m.id.String() + "/status",
					ip:     fmt.Sprintf("10.%d.12.1", i),
					token:  m.manageTok,
				},
			)
			expect("status", code, http.StatusOK)
		}))
	}
	requests.Wait()
	requestsDone.Store(true)
	close(stop)
	background.Wait()
	if overlapping.Load() == 0 {
		t.Error("no sweep-once process ran while the requests ran")
	}
	for i, m := range messages {
		claims, goneAcks, burns, cancels := got[i].claims.Load(), got[i].goneAcks.Load(), got[i].burns.Load(), got[i].cancels.Load()
		opens := int32(m.policy.OpensMax)
		if claims > opens || burns+cancels > 1 {
			t.Errorf("message %d: %d claims of %d opens, %d burns, %d cancels", i, claims, opens, burns, cancels)
			continue
		}
		if burns+cancels == 0 && (claims != opens || goneAcks != 0) {
			t.Errorf(
				"message %d: with four readers and no deletion, %d of %d opens were claimed and %d acks were refused",
				i,
				claims,
				opens,
				goneAcks,
			)
		}
		_, st := h.status(m)
		want := "available"
		switch {
		case cancels == 1:
			want = "cancelled"
		case burns == 1:
			want = "deleted"
		case claims == int32(m.policy.OpensMax):
			want = "opened"
		}
		if st["state"] != want {
			t.Errorf(
				"message %d ended as %v, want %s (%d claims of %d opens, %d burns, %d cancels)",
				i,
				st["state"],
				want,
				claims,
				opens,
				burns,
				cancels,
			)
		}
		if ended := want != "available"; h.blobExists(m.id) == ended {
			t.Errorf("message %d in state %s: blob present %v", i, want, !ended)
		}
	}
	h.clock.Advance(2 * time.Hour)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, m := range messages {
		if h.blobExists(m.id) {
			t.Errorf("message %d still has its blob after it expired and was swept", i)
		}
	}
}
