package abuse

import (
	"crypto/hpke"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
)

const (
	Retention       = 7 * 24 * time.Hour
	directoryName   = "abuse"
	FileExtension   = ".sealed"
	legacyExtension = ".jsonl"
	legacyFile      = "abuse-reports.jsonl"
	maxFilesBytes   = 16 << 20
)

var ErrFull = errors.New("server: abuse report files reached their size limit")

type Reports struct {
	directory string
	key       hpke.PublicKey
	logger    *slog.Logger
	mutex     sync.Mutex
}

func NewReports(dataDir string, key hpke.PublicKey, logger *slog.Logger) *Reports {
	return &Reports{directory: Directory(dataDir), key: key, logger: logger}
}

func Directory(dataDir string) string {
	return filepath.Join(dataDir, directoryName)
}

// Record seals the report to the operator's public key and appends it to the file of its UTC day.
func (r *Reports) Record(now time.Time, report *Report) error {
	line, err := Seal(r.key, now, report)
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
	path := filepath.Join(r.directory, reportDate(now)+FileExtension)
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

// Prune deletes the day files older than the retention, sealed or from older builds.
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
	day, sealed := strings.CutSuffix(name, FileExtension)
	if !sealed {
		day, sealed = strings.CutSuffix(name, legacyExtension)
	}
	if _, err := time.Parse(time.DateOnly, day); !sealed || err != nil {
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
