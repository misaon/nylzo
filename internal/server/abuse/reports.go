package abuse

import (
	"crypto/hpke"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const (
	Retention       = 7 * 24 * time.Hour
	directoryName   = "abuse"
	FileExtension   = ".sealed"
	legacyExtension = ".jsonl"
	legacyFile      = "abuse-reports.jsonl"
	maxFilesBytes   = 64 << 20
	keptDays        = 8
)

var (
	ErrFull          = errors.New("server: abuse report files reached their size limit")
	ErrLegacyReports = errors.New(
		"abuse: the data directory holds abuse reports in the clear from an unreleased development build; " +
			"nylzo does not convert them, so remove them from the data directory",
	)
)

type Reports struct {
	directory string
	key       hpke.PublicKey
	logger    *slog.Logger
	maxBytes  int64
	warnedOn  string
	mutex     sync.Mutex
}

// FilesBytes is room for every report the global cap admits, 3 per message for 8 days of creates, so the reports
// about one sender's own messages cannot crowd out a notice; without a cap the files hold 64 MiB (F6).
func FilesBytes(dailyCreates, reportsPerMessage int64) int64 {
	if dailyCreates <= 0 {
		return maxFilesBytes
	}
	return max(maxFilesBytes, reportsPerMessage*dailyCreates*keptDays*SealedLen)
}

func NewReports(dataDir string, key hpke.PublicKey, logger *slog.Logger, maxBytes int64) *Reports {
	return &Reports{directory: Directory(dataDir), key: key, logger: logger, maxBytes: maxBytes}
}

func Directory(dataDir string) string {
	return filepath.Join(dataDir, directoryName)
}

// Record seals the report to the operator's public key and appends it to the file of its UTC day.
func (r *Reports) Record(now time.Time, report *Report) error {
	sealed, err := Seal(r.key, now, report)
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
	if used+int64(len(sealed)) > r.maxBytes {
		return ErrFull
	}
	if today := reportDate(now); 2*(used+int64(len(sealed))) > r.maxBytes && r.warnedOn != today {
		r.warnedOn = today
		r.logger.Warn("abuse-files-half-full", "bytes", used, "limit", r.maxBytes)
	}
	return r.appendToDay(reportDate(now), sealed)
}

// appendToDay writes at the end of the whole reports and cuts a failed write off again, so one report never hides the next.
func (r *Reports) appendToDay(day string, sealed []byte) error {
	path := filepath.Join(r.directory, day+FileExtension)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	whole := info.Size() - info.Size()%SealedLen
	if whole != info.Size() {
		r.logger.Warn("abuse-tail-truncated", "bytes", info.Size()-whole)
	}
	_, writeErr := file.WriteAt(sealed, whole)
	if writeErr == nil {
		writeErr = file.Truncate(whole + int64(len(sealed)))
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if writeErr != nil {
		writeErr = errors.Join(writeErr, file.Truncate(whole), file.Sync())
	}
	if closeErr := errors.Join(writeErr, file.Close()); closeErr != nil {
		return closeErr
	}
	start, err := time.Parse(time.DateOnly, day)
	if err == nil {
		err = errors.Join(os.Chtimes(path, start, start), os.Chtimes(r.directory, start, start))
	}
	if err != nil {
		r.logger.Error("abuse-times", "err", logging.ErrorText(err))
	}
	return nil
}

func RefuseLegacyReports(dataDir string) error {
	_, err := os.Lstat(filepath.Join(dataDir, legacyFile))
	switch {
	case err == nil:
		return fmt.Errorf("%w: %s", ErrLegacyReports, legacyFile)
	case !errors.Is(err, fs.ErrNotExist):
		return err
	default:
	}
	entries, err := os.ReadDir(Directory(dataDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), legacyExtension) {
			return fmt.Errorf("%w: %s", ErrLegacyReports, filepath.Join(directoryName, entry.Name()))
		}
	}
	return nil
}

// Restamp sets the times of the day files and their directory again, so no ctime shows when a report arrived.
func Restamp(dataDir string, now time.Time) error {
	directory := Directory(dataDir)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		day, ok := reportDay(entry.Name())
		if !ok {
			continue
		}
		start, parseErr := time.Parse(time.DateOnly, day)
		if parseErr != nil {
			continue
		}
		if chtimesErr := os.Chtimes(filepath.Join(directory, entry.Name()), start, start); !errors.Is(chtimesErr, fs.ErrNotExist) {
			errs = append(errs, chtimesErr)
		}
	}
	quarter := now.Truncate(store.BlobPurgeStep)
	return errors.Join(append(errs, os.Chtimes(directory, quarter, quarter))...)
}

// Prune deletes the day files older than the retention.
func Prune(dataDir string, now time.Time) error {
	return pruneDirectory(Directory(dataDir), now)
}

func pruneDirectory(directory string, now time.Time) error {
	cutoff := now.Add(-Retention)
	var errs []error
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
