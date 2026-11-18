package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/adminapi"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/diskspace"
	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/leasetimer"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/server/visits"
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store"
)

const (
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 10 * time.Second
	createBodyTimeout   = 120 * time.Second
	writeTimeout        = 120 * time.Second
	idleTimeout         = 30 * time.Second
	maxHeaderBytes      = 8 << 10
	maxHeaderValues     = 64
	h2cMaxStreams       = 32
	h2cStreamBuffer     = 64 << 10
	h2cConnectionBuffer = 1 << 20
	h2cMaxFrame         = 16 << 10
)

// ErrEpochInDataDirectory stops a server whose data directory still holds the epoch directory NYLZO_EPOCH_DIR replaced.
var ErrEpochInDataDirectory = errors.New("server: the data directory still holds an epoch directory")

type Option func(*Server)

func WithLogger(logger *slog.Logger) Option {
	return func(server *Server) {
		if logger != nil {
			server.logger = logger
		}
	}
}

func WithWebFiles(files fs.FS) Option {
	return func(server *Server) {
		if files != nil {
			server.webFiles = files
		}
	}
}

type Server struct {
	settings      config.Server
	store         store.Store
	blobs         store.Blobs
	ring          *pepper.Ring
	recordsKey    seal.Key
	epochs        *epoch.Ratchet
	leaseTimers   *leasetimer.Timers
	logger        *slog.Logger
	instance      *ratelimit.Fair
	sourceLimiter *ratelimit.Limiter
	rangeLimiter  *ratelimit.Limiter
	addressKey    *ratelimit.AddressKey
	proxies       ratelimit.Proxies
	counters      *ratelimit.Counters
	streams       *ratelimit.Streams
	challenges    *proof.Challenges
	work          *proof.Work
	idTokens      proof.IDTokens
	disk          *diskspace.Reserver
	dummyRow      store.Row
	webFiles      fs.FS
	frontend      http.Handler
	requestCounts *logging.RequestCounts
	eventCounts   *logging.Events
	events        *slog.Logger
	handler       http.Handler
	adminStore    *adminstore.Store
	admin         *adminapi.API
	counts        *statistics.Counts
	visits        *visits.Tracker
	usage         *hostusage.Sampler
	refusals      *ratelimit.Refusals
	// documentReports is the one budget of the edge's reports about served pages.
	documentReports *ratelimit.Limiter
	countries       *countries
}

func New(settings *config.Server, storage store.Store, blobs store.Blobs, options ...Option) (*Server, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	ring, err := pepper.NewRing(settings.Pepper, settings.PepperPrevious)
	if err != nil {
		return nil, err
	}
	server := &Server{
		settings:    *settings,
		store:       storage,
		blobs:       blobs,
		ring:        ring,
		leaseTimers: leasetimer.New(),
		logger:      logging.New(os.Stderr),
		webFiles:    web.Files(),
	}
	server.recordsKey = ring.RecordsKey()
	server.dummyRow = newDummyRow(server.recordsKey)
	server.settings.Pepper, server.settings.PepperPrevious = nil, nil
	for _, option := range options {
		option(server)
	}
	if loadErr := server.loadWebClient(); loadErr != nil {
		return nil, loadErr
	}
	server.requestCounts = logging.NewRequestCounts(server.logger)
	server.eventCounts = logging.NewEvents(server.logger)
	server.events = server.eventCounts.Logger()
	if ratchetErr := server.openRatchet(); ratchetErr != nil {
		return nil, ratchetErr
	}
	server.setUpLimits()
	server.challenges = proof.NewChallenges(
		ring,
		proof.Capacity(settings.Rates.BucketPerSecond, settings.Rates.BucketBurst),
	)
	server.work = proof.NewWork(ring, storage, settings.PoWConcurrency)
	server.idTokens = proof.NewIDTokens(ring)
	server.disk = diskspace.NewReserver(settings.DataDirectory, settings.MinFreeBytes, diskspace.Free, server.events)
	server.setUpAdmin()
	server.handler = server.routes()
	return server, nil
}

// Close counts the pages that still wait for a sign of a person as unconfirmed and keeps the counts for the next
// start, so a restart loses none.
func (s *Server) Close() {
	now := time.Now()
	s.visits.Stop(now)
	if err := s.counts.Stop(context.Background(), now); err != nil {
		s.logger.Error("admin-counts", "err", logging.ErrorText(err))
	}
	s.leaseTimers.Stop()
	s.epochs.Close()
}

// Events logs what happens to a request or a message: its lines wait for the hourly flush of RunHousekeeping.
func (s *Server) Events() *slog.Logger {
	return s.events
}

// RunRatchet erases the epoch key of every minute that has passed when the next one starts, so a share goes at the
// deadline of its message; it also catches up at once on the minutes that a slow start let pass. It waits for the next
// minute of the wall clock each time, so a suspend or a clock step delays it by a minute at most.
func (s *Server) RunRatchet(ctx context.Context) {
	for {
		if err := s.advanceRatchet(time.Now()); err != nil {
			s.logger.ErrorContext(ctx, "epoch-advance", "err", logging.ErrorText(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(untilNextMinute(time.Now())):
		}
	}
}

func untilNextMinute(now time.Time) time.Duration {
	return now.Truncate(time.Minute).Add(time.Minute).Sub(now)
}

func (s *Server) HTTPServer() *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Addr:                  s.settings.Listen,
		Handler:               s.handler,
		ReadHeaderTimeout:     readHeaderTimeout,
		ReadTimeout:           readTimeout,
		WriteTimeout:          writeTimeout,
		IdleTimeout:           idleTimeout,
		MaxHeaderBytes:        maxHeaderBytes,
		MaxHeaderValueCount:   maxHeaderValues,
		DisableClientPriority: true,
		Protocols:             &protocols,
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams:          h2cMaxStreams,
			MaxReceiveBufferPerConnection: h2cConnectionBuffer,
			MaxReceiveBufferPerStream:     h2cStreamBuffer,
			MaxReadFrameSize:              h2cMaxFrame,
		},
		ErrorLog: logging.NewHTTPErrorLog(s.eventCounts),
	}
}

func (s *Server) RunSweeper(ctx context.Context) {
	if s.settings.SweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.settings.SweepInterval)
	defer ticker.Stop()
	for {
		s.recheckEpochStorage(ctx)
		_, err := s.sweepOnce(ctx)
		switch {
		case ctx.Err() != nil:
			s.logger.InfoContext(ctx, "sweep-stopped")
			return
		case err != nil:
			s.logger.ErrorContext(ctx, "sweep", "err", sweep.FirstError(err))
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunHousekeeping works at every minute boundary: it writes the request counts and events of a window that ended,
// drops rate-limit entries that no longer limit anything and request challenges that can no longer be used, and draws
// a new address key when a UTC day begins (spec §5.3, §5.5, §10). It drops the counts of the last window at shutdown, which would end that window early.
func (s *Server) RunHousekeeping(ctx context.Context) {
	day := ratelimit.Day(time.Now()).Start
	window := logging.StartWindow(time.Now())
	for {
		now := time.Now()
		select {
		case <-ctx.Done():
			return
		case <-time.After(untilNextMinute(now)):
		}
		now = time.Now()
		if window.Ended(now) {
			s.flushCounts(ctx)
		}
		newDay := !ratelimit.Day(now).Start.Equal(day)
		if newDay {
			day = ratelimit.Day(now).Start
			s.addressKey.Rotate()
			s.forgetAddresses()
		} else {
			s.purgeRateTables(now)
		}
		s.keepAdmin(ctx, now, newDay)
		s.purgeChallenges(now)
		eraseUnreachableSecrets()
	}
}

func (s *Server) loadWebClient() error {
	build, err := webclient.Load(s.webFiles)
	if err != nil {
		return err
	}
	if s.frontend, err = webclient.NewHandler(
		build,
		s.settings.PublicURL,
		s.settings.Operator.Contact,
		s.buildInformation(),
	); err != nil {
		return err
	}
	s.logger.Info("web-client", "built", build.Built, "assets", len(build.Assets), "public_url", s.settings.PublicURL)
	return nil
}

func (s *Server) flushCounts(ctx context.Context) {
	s.requestCounts.Flush(ctx)
	s.eventCounts.Flush(ctx)
}

// advanceRatchet runs in secret mode, so the derivation leaves no copy of the passed seed in memory.
func (s *Server) advanceRatchet(now time.Time) error {
	var err error
	secretmode.Do(func() { err = s.epochs.Advance(now) })
	return err
}

// eraseUnreachableSecrets runs a collection, which is when secret mode erases what an ended request allocated; an
// idle server would otherwise wait up to two minutes for the runtime's forced collection.
func eraseUnreachableSecrets() {
	runtime.GC()
}

// Secret mode makes the runtime erase the replaced maps, which held address buckets, once they are unreachable.
func (s *Server) purgeRateTables(now time.Time) {
	secretmode.Do(func() {
		s.sourceLimiter.Purge(now)
		s.rangeLimiter.Purge(now)
		s.counters.Purge(now)
		s.instance.Purge(now)
		s.streams.Purge()
	})
}

// purgeChallenges runs in secret mode, so the runtime erases the replaced table of challenges once it is unreachable.
func (s *Server) purgeChallenges(now time.Time) {
	secretmode.Do(func() { s.challenges.Purge(now) })
}

func (s *Server) forgetAddresses() {
	secretmode.Do(func() {
		s.sourceLimiter.Reset()
		s.rangeLimiter.Reset()
		s.counters.Reset()
		s.instance.Reset()
		s.streams.Purge()
	})
}

func (s *Server) sweepOnce(ctx context.Context) (store.SweepReport, error) {
	now := time.Now()
	report, err := sweep.Run(ctx, s.store, s.blobs, now)
	s.count(statistics.MetricExpired, int64(report.Blobs))
	return report, errors.Join(err, s.advanceRatchet(now))
}

// recheckEpochStorage repeats the start check of the epoch directory, so swap turned on later reaches the log; the
// pinned seed stays out of swap meanwhile, but not once serve stops.
func (s *Server) recheckEpochStorage(ctx context.Context) {
	if s.settings.Development.AllowUnerasableStorage {
		return
	}
	if err := erase.RequireInPlace(epoch.ErasablePaths(s.settings.EpochDirectory())...); err != nil {
		s.logger.ErrorContext(ctx, "epoch-storage-not-erasable", "err", logging.ErrorText(err))
	}
}

func (s *Server) setUpLimits() {
	rates := s.settings.Rates
	s.instance = ratelimit.NewFair(rates.BucketPerSecond, rates.BucketBurst)
	s.sourceLimiter = ratelimit.NewLimiter(rates.SourcePerSecond, rates.SourceBurst)
	s.rangeLimiter = ratelimit.NewLimiter(rates.BucketPerSecond*rangeShare, max(1, rates.BucketBurst*rangeShare))
	s.addressKey = ratelimit.NewAddressKey()
	s.proxies = ratelimit.Proxies{
		Trusted:    s.settings.TrustedProxies,
		ReadRealIP: s.settings.ProxyMode == config.ProxyModeScaleway,
	}
	s.counters = ratelimit.NewCounters()
	s.streams = ratelimit.NewStreams(s.settings.MaxStreams)
}

// openRatchet starts a new ratchet only in a directory without a seed, which ends every live message.
func (s *Server) openRatchet() error {
	if err := PrepareEpochDirectory(&s.settings); err != nil {
		return err
	}
	options := epoch.Options{Create: true, CheckFileSystem: s.requireErasableEpochDirectory}
	var (
		ratchet *epoch.Ratchet
		created bool
		err     error
	)
	secretmode.Do(func() { ratchet, created, err = epoch.Open(s.settings.EpochDirectory(), time.Now(), options) })
	if err != nil {
		return err
	}
	if created {
		s.logger.Warn(
			"epoch-seed-created",
			"notice",
			"a new seed: messages created under an earlier one can no longer be opened and end at the next request about them",
		)
	}
	if err = errors.Join(s.pinSeed(ratchet), s.warnIfSeedOnDisk()); err != nil {
		ratchet.Close()
		return err
	}
	s.epochs = ratchet
	return nil
}

// pinSeed keeps the seed file out of swap turned on after the start; NYLZO_ALLOW_SWAP locks no memory at all.
func (s *Server) pinSeed(ratchet *epoch.Ratchet) error {
	if s.settings.Development.AllowSwap {
		return nil
	}
	return ratchet.Pin()
}

func (s *Server) warnIfSeedOnDisk() error {
	onTmpfs, err := erase.OnTmpfs(s.settings.EpochDirectory())
	if err != nil || onTmpfs {
		return err
	}
	s.logger.Warn(
		"epoch-seed-on-disk",
		"notice",
		"every disk image and backup of the epoch directory holds the seed and opens the messages live when it "+
			"was taken; keep NYLZO_EPOCH_DIR on a tmpfs",
	)
	return nil
}

// PrepareEpochDirectory creates the default epoch directory inside the data directory. A directory that the
// operator names must exist already, so a volume that failed to mount stops the server instead of a new seed.
func PrepareEpochDirectory(settings *config.Server) error {
	if settings.EpochDirectoryOverride != "" {
		return RefuseEpochInDataDirectory(settings)
	}
	return os.MkdirAll(settings.EpochDirectory(), 0o700)
}

// RefuseEpochInDataDirectory refuses the epoch directory of the data directory while NYLZO_EPOCH_DIR names another
// directory that exists. Nothing moves a seed there forward any more, under any file name, so it keeps the shares of
// the messages that were live when it was last written, and every backup of the data directory copies them.
func RefuseEpochInDataDirectory(settings *config.Server) error {
	if settings.EpochDirectoryOverride == "" {
		return nil
	}
	inData := settings.DataEpochDirectory()
	found, err := os.Stat(inData)
	if err != nil {
		return leftoverCheckError(err)
	}
	named, err := os.Stat(settings.EpochDirectoryOverride)
	if err != nil || os.SameFile(found, named) {
		return leftoverCheckError(err)
	}
	advice := leftoverAdvice(inData, settings.EpochDirectoryOverride)
	return fmt.Errorf("%w: %s; %s", ErrEpochInDataDirectory, inData, advice)
}

// leftoverAdvice offers the copy only while NYLZO_EPOCH_DIR holds no seed, which the copy would overwrite, and never
// for a damaged seed or one of a release before 0.14.0, which serve erases with its messages. A seed that fails
// another check keeps the offer and names the check.
func leftoverAdvice(inData, named string) string {
	const eraseAll = "erase every file in it in place (shred --zero --iterations=0 --remove=unlink) and remove it"
	if !exists(filepath.Join(inData, epoch.FileName)) || exists(filepath.Join(named, epoch.FileName)) {
		return eraseAll
	}
	var (
		held epoch.State
		err  error
	)
	secretmode.Do(func() { held, err = epoch.Inspect(inData) })
	copySeed := "copy its seed into " + named + " first to keep the live messages, then " + eraseAll
	switch {
	case errors.Is(err, epoch.ErrCorrupt), err == nil && held != epoch.StateSeed:
		return eraseAll
	case err != nil:
		return "fix the seed (" + err.Error() + "), " + copySeed
	default:
		return copySeed
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// leftoverCheckError leaves a missing NYLZO_EPOCH_DIR to epoch.Open, which refuses it.
func leftoverCheckError(err error) error {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("server: check for an epoch directory left in the data directory: %w", err)
}

func (s *Server) requireErasableEpochDirectory(directory string) error {
	err := erase.RequireInPlace(epoch.ErasablePaths(directory)...)
	if errors.Is(err, erase.ErrNotInPlace) && s.settings.Development.AllowUnerasableStorage {
		s.logger.Warn("storage-not-erasable", "directory", "epoch")
		return nil
	}
	return err
}
