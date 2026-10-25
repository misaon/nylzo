package abuse

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	Retention        = 7 * 24 * time.Hour
	directoryName    = "abuse"
	fileExtension    = ".jsonl"
	legacyFile       = "abuse-reports.jsonl"
	maxFilesBytes    = 16 << 20
	perMessagePerDay = 3
	counterKeyPrefix = "abuse:id:"
)

var ErrFull = errors.New("server: abuse report files reached their size limit")

type Reports struct {
	directory string
	counters  store.Counters
	logger    *slog.Logger
	mutex     sync.Mutex
}

func NewReports(dataDir string, counters store.Counters, logger *slog.Logger) *Reports {
	return &Reports{directory: Directory(dataDir), counters: counters, logger: logger}
}

func Directory(dataDir string) string {
	return filepath.Join(dataDir, directoryName)
}

// Record stores at most three reports per message and UTC day and keeps the count only for stored reports.
func (r *Reports) Record(ctx context.Context, now time.Time, id, reason string) error {
	day := ratelimit.Day(now)
	key := counterKeyPrefix + id
	count, err := r.counters.RateReserve(
		context.WithoutCancel(ctx),
		store.RateHit{Key: key, WindowStart: day.Start, PurgeAt: day.PurgeAt(), Limit: perMessagePerDay},
	)
	if err != nil || count == 0 {
		return err
	}
	appendErr := r.append(now, id, reason)
	if appendErr == nil {
		return nil
	}
	if releaseErr := r.counters.RateRelease(context.WithoutCancel(ctx), key, day.Start); releaseErr != nil {
		r.logger.ErrorContext(ctx, "abuse-release", "err", logging.ErrorText(releaseErr))
	}
	return appendErr
}

func (r *Reports) append(now time.Time, id, reason string) error {
	line, err := json.Marshal(map[string]string{"date": reportDate(now), "id": id, "reason": core.RedactReason(reason)})
	if err != nil {
		return err
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if pruneErr := pruneDirectory(r.directory, now); pruneErr != nil {
		return pruneErr
	}
	if mkdirErr := os.MkdirAll(r.directory, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	used, err := filesBytes(r.directory)
	if err != nil {
		return err
	}
	if used+int64(len(line))+1 > maxFilesBytes {
		return ErrFull
	}
	path := filepath.Join(r.directory, reportDate(now)+fileExtension)
	if writeErr := appendLine(path, line); writeErr != nil {
		return writeErr
	}
	start := ratelimit.Day(now).Start
	if timesErr := errors.Join(os.Chtimes(path, start, start), os.Chtimes(r.directory, start, start)); timesErr != nil {
		r.logger.Error("abuse-times", "err", logging.ErrorText(timesErr))
	}
	return nil
}

func appendLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Sync(), file.Close())
}

// Prune deletes the day files older than the retention and the report file of older builds.
func Prune(dataDir string, now time.Time) error {
	return pruneDirectory(Directory(dataDir), now)
}

func pruneDirectory(directory string, now time.Time) error {
	cutoff := now.Add(-Retention)
	var errs []error
	legacy := filepath.Join(filepath.Dir(directory), legacyFile)
	if info, err := os.Stat(legacy); err == nil && info.ModTime().Before(cutoff) {
		errs = append(errs, removeFile(legacy))
	}
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, entry := range entries {
		if day, ok := reportDay(entry.Name()); ok && day <= reportDate(cutoff) {
			errs = append(errs, removeFile(filepath.Join(directory, entry.Name())))
		}
	}
	return errors.Join(errs...)
}

func filesBytes(directory string) (int64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if _, ok := reportDay(entry.Name()); !ok {
			continue
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, fs.ErrNotExist) {
			continue
		}
		if infoErr != nil {
			return 0, infoErr
		}
		total += info.Size()
	}
	return total, nil
}

func reportDate(moment time.Time) string {
	return moment.UTC().Format(time.DateOnly)
}

func reportDay(name string) (string, bool) {
	day, ok := strings.CutSuffix(name, fileExtension)
	if _, err := time.Parse(time.DateOnly, day); !ok || err != nil {
		return "", false
	}
	return day, true
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
