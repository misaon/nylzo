package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	abuseDirName    = "abuse"
	abuseFileExt    = ".jsonl"
	legacyAbuseFile = "abuse-reports.jsonl"
	abuseFileLimit  = 16 << 20
	abuseRetention  = 7 * 24 * time.Hour
	abusePerRange   = 3
)

type abuseBody struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (s *Server) handleAbuse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	var body abuseBody
	if err != nil || json.Unmarshal(raw, &body) != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if _, err := core.ParseID(body.ID); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		return
	}
	if !s.allowHourly(w, r, "abuse", s.cfg.Rates.AbusePerHour) {
		return
	}
	now := s.clock()
	a, ok, err := s.authorize(ctx, r, body.ID, pepper.Read, raw)
	id := a.id
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if ok && a.rec.Message != nil && !ended(a.rec.Message, now) {
		err := s.recordAbuse(ctx, now, id.String(), body.Reason)
		if errors.Is(err, errAbuseFull) {
			s.log.Error("abuse-file-full")
			setRetryAfter(w, abuseFullRetry)
			writeError(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
		if err != nil {
			s.log.Error("abuse-record", "err", ErrorText(err))
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

var errAbuseFull = errors.New("server: abuse report files reached their size limit")

func (s *Server) recordAbuse(ctx context.Context, now time.Time, id, reason string) error {
	d := ratelimit.Day(now)
	key := "abuse:id:" + id
	_, ok, err := s.store.RateReserve(
		context.WithoutCancel(ctx),
		store.RateHit{Key: key, WindowStart: d.Start, PurgeAt: d.PurgeAt(), Limit: abusePerRange},
	)
	if err != nil || !ok {
		return err
	}
	if err := s.appendAbuse(now, id, reason); err != nil {
		if rerr := s.store.RateRelease(context.WithoutCancel(ctx), key, d.Start); rerr != nil {
			s.log.Error("abuse-release", "err", ErrorText(rerr))
		}
		return err
	}
	return nil
}

func (s *Server) appendAbuse(now time.Time, id, reason string) error {
	line, err := json.Marshal(map[string]string{"date": reportDate(now), "id": id, "reason": core.RedactReason(reason)})
	if err != nil {
		return err
	}
	s.abuseMu.Lock()
	defer s.abuseMu.Unlock()
	if err := pruneAbuse(s.abuseDir, now); err != nil {
		return err
	}
	if err := os.MkdirAll(s.abuseDir, 0o700); err != nil {
		return err
	}
	used, err := abuseBytes(s.abuseDir)
	if err != nil {
		return err
	}
	if used+int64(len(line))+1 > abuseFileLimit {
		return errAbuseFull
	}
	path := filepath.Join(s.abuseDir, reportDate(now)+abuseFileExt)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if err = errors.Join(err, f.Sync(), f.Close()); err != nil {
		return err
	}
	day := ratelimit.Day(now).Start
	if err := errors.Join(os.Chtimes(path, day, day), os.Chtimes(s.abuseDir, day, day)); err != nil {
		s.log.Error("abuse-times", "err", ErrorText(err))
	}
	return nil
}

func reportDate(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}

func abuseDir(dataDir string) string {
	return filepath.Join(dataDir, abuseDirName)
}

func abuseDay(name string) (string, bool) {
	day, ok := strings.CutSuffix(name, abuseFileExt)
	if _, err := time.Parse(time.DateOnly, day); !ok || err != nil {
		return "", false
	}
	return day, true
}

func abuseBytes(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		if _, ok := abuseDay(e.Name()); !ok {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}

func pruneAbuse(dir string, now time.Time) error {
	cutoff := now.Add(-abuseRetention)
	var errs []error
	legacy := filepath.Join(filepath.Dir(dir), legacyAbuseFile)
	if info, err := os.Stat(legacy); err == nil && info.ModTime().Before(cutoff) {
		errs = append(errs, removeReport(legacy))
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if day, ok := abuseDay(e.Name()); ok && day <= reportDate(cutoff) {
			errs = append(errs, removeReport(filepath.Join(dir, e.Name())))
		}
	}
	return errors.Join(errs...)
}

func removeReport(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
