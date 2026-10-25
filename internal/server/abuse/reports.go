package abuse

import (
	"crypto/hpke"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/logging"
)

const (
	Retention       = 7 * 24 * time.Hour
	directoryName   = "abuse"
	FileExtension   = ".sealed"
	legacyExtension = ".jsonl"
	legacyFile      = "abuse-reports.jsonl"
	maxFilesBytes   = 64 << 20
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
	if used+int64(len(sealed)) > maxFilesBytes {
		return ErrFull
	}
	return r.appendToDay(reportDate(now), sealed)
}

// SealLegacy seals the reports that older builds kept in the clear, then deletes their files.
func (r *Reports) SealLegacy() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	legacy := []string{filepath.Join(filepath.Dir(r.directory), legacyFile)}
	entries, err := os.ReadDir(r.directory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), legacyExtension) {
			legacy = append(legacy, filepath.Join(r.directory, entry.Name()))
		}
	}
	var errs []error
	for _, path := range legacy {
		errs = append(errs, r.sealLegacyFile(path))
	}
	return errors.Join(errs...)
}

func (r *Reports) sealLegacyFile(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	byDay := map[string][]byte{}
	for line := range strings.Lines(string(data)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var stored Stored
		if decodeErr := json.Unmarshal([]byte(line), &stored); decodeErr != nil {
			return decodeErr
		}
		if _, dateErr := time.Parse(time.DateOnly, stored.Date); dateErr != nil {
			return dateErr
		}
		sealed, sealErr := sealStored(r.key, &stored)
		if sealErr != nil {
			return sealErr
		}
		byDay[stored.Date] = append(byDay[stored.Date], sealed...)
	}
	if mkdirErr := os.MkdirAll(r.directory, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	for day, sealed := range byDay {
		if appendErr := r.appendToDay(day, sealed); appendErr != nil {
			return appendErr
		}
	}
	return removeFile(path)
}

func (r *Reports) appendToDay(day string, sealed []byte) error {
	path := filepath.Join(r.directory, day+FileExtension)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(sealed)
	if closeErr := errors.Join(writeErr, file.Sync(), file.Close()); closeErr != nil {
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
