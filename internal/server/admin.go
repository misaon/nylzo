package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/adminapi"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/country"
	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/server/visits"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	maxVisitBytes = 256
	usageInterval = 5 * time.Second
	usageKept     = adminapi.UsageShown

	reasonRequests = "requests"
	reasonCreate   = "create"
)

// WithAdminStore gives the server the store of the operator's admin area; without one it has no admin area and
// counts nothing.
func WithAdminStore(store *adminstore.Store) Option {
	return func(server *Server) {
		server.adminStore = store
	}
}

// countries loads the table at the first lookup, because a server whose admin area nobody uses never needs it.
type countries struct {
	load func() *country.Table
}

func newCountries(load func() (*country.Table, error), report func(error)) *countries {
	return &countries{load: sync.OnceValue(func() *country.Table {
		table, err := load()
		if err != nil {
			report(err)
		}
		return table
	})}
}

func (c *countries) Lookup(address netip.Addr) country.Code {
	if table := c.load(); table != nil {
		return table.Lookup(address)
	}
	return country.Code{}
}

func (c *countries) Source() country.Source {
	if table := c.load(); table != nil {
		return table.Source()
	}
	return country.Source{}
}

func (s *Server) setUpAdmin() {
	s.refusals = ratelimit.NewRefusals()
	s.documentReports = ratelimit.NewLimiter(api.DocumentReportsPerSecond, api.DocumentReportsBurst)
	s.usage = hostusage.NewSampler(usageKept)
	s.countries = newCountries(country.Load, s.reportCountryTable)
	s.counts = statistics.New(s.adminStore)
	s.visits = visits.NewTracker(s.counts, s.countries, time.Now())
	if s.adminStore == nil {
		return
	}
	s.admin = adminapi.New(&adminapi.Config{
		Store:         s.adminStore,
		PublicURL:     s.settings.PublicURL,
		Challenges:    s.challenges,
		Spent:         spentBy,
		Counts:        s.counts,
		Visits:        s.visits,
		Usage:         s.usage,
		Refusals:      s.refusals,
		Countries:     s.countries,
		Stored:        s.storedMessages,
		DataDirectory: s.settings.DataDirectory,
		ReserveBytes:  s.settings.MinFreeBytes,
		Version:       s.settings.Version,
		Events:        s.events,
		Refuse:        s.noteRefusal,
	})
}

func (s *Server) reportCountryTable(err error) {
	s.events.Error("country-table", "err", logging.ErrorText(err))
}

// StartCounting switches the counting on when a passkey can already sign in to see the counts.
func (s *Server) StartCounting(ctx context.Context) error {
	if s.admin == nil {
		return nil
	}
	return errors.Join(s.counts.Restore(ctx), s.admin.Sync(ctx, time.Now()))
}

// storedMessages leaves out the dummy row, which every store holds and which names a blob like a message does.
func (s *Server) storedMessages(ctx context.Context) (int, error) {
	named, err := s.store.CountBlobReferences(ctx)
	return max(0, named-1), err
}

// count adds to a figure of the admin area; it does nothing on a server that has no admin area in use.
func (s *Server) count(metric string, value int64) {
	s.counts.Add(metric, value, time.Now())
}

// noteRefusal keeps the address of a client that a limit turned away, in memory, for the operator to see. A server
// whose admin area nobody uses keeps none.
func (s *Server) noteRefusal(r *http.Request, reason string) {
	if !s.counts.Enabled() {
		return
	}
	now := time.Now()
	s.refusals.Note(s.bucketsOf(r).address, reason, now)
	s.counts.Add(statistics.MetricRefusals, 1, now)
}

func (s *Server) refuseLimited(w http.ResponseWriter, r *http.Request, reason string, wait time.Duration) {
	s.noteRefusal(r, reason)
	writeRateLimited(w, wait)
}

// handleVisit takes the signal of a page about its visitor and answers the same whatever came of it; only the
// party that served a page reports the document (spec section 6.7).
func (s *Server) handleVisit(w http.ResponseWriter, r *http.Request) {
	visit, ok := readVisit(w, r)
	if !ok {
		return
	}
	if visit.Signal != api.VisitDocument {
		s.recordVisit(r, &visit)
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleDocumentVisit takes the report of the edge about a page it served. It answers a client as an address that
// does not exist, and it has one budget for all reports instead of the limits of the visitor (spec section 6.7).
func (s *Server) handleDocumentVisit(w http.ResponseWriter, r *http.Request) {
	if !s.fromEdge(r) {
		writeNotFound(w)
		return
	}
	visit, ok := readVisit(w, r)
	if !ok {
		return
	}
	if s.documentReports.Allow(ratelimit.Bucket{}, time.Now()) {
		visit.Signal = api.VisitDocument
		s.recordVisit(r, &visit)
	}
	writeJSON(w, http.StatusOK, api.DocumentVisits{Counted: s.counts.Enabled()})
}

// fromEdge holds for a report of the nylzo frontend. Another reverse proxy passes every address of the API on, so
// the server takes reports only where NYLZO_EDGE_REPORTS says that the trusted proxy is the frontend.
func (s *Server) fromEdge(r *http.Request) bool {
	return s.settings.EdgeReports && r.URL.Path == api.DocumentVisitsPath && s.proxies.FromTrustedProxy(r)
}

func readVisit(w http.ResponseWriter, r *http.Request) (api.Visit, bool) {
	var visit api.Visit
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVisitBytes))
	if err != nil || json.Unmarshal(body, &visit) != nil {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return api.Visit{}, false
	}
	return visit, true
}

func (s *Server) recordVisit(r *http.Request, visit *api.Visit) {
	if !s.counts.Enabled() {
		return
	}
	s.visits.Record(&visits.Visit{
		Signal:     visit.Signal,
		Page:       visit.Page,
		Navigation: visit.Navigation,
		Address:    s.bucketsOf(r).address,
		UserAgent:  r.UserAgent(),
	}, time.Now())
}

// countDocuments reports the pages this server serves itself; behind the edge, the edge reports them.
func (s *Server) countDocuments(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if visit, counted := webclient.DocumentVisit(r); counted {
			s.recordVisit(r, &visit)
		}
		next.ServeHTTP(w, r)
	})
}

// keepAdmin does the minute's work of the admin area: it settles the visits, forgets the clients whose refusal is
// an hour old, ends the sessions that ran out and writes the counts. Secret mode erases the tables it replaces.
func (s *Server) keepAdmin(ctx context.Context, now time.Time, newDay bool) {
	secretmode.Do(func() {
		s.visits.Settle(now)
		if newDay {
			s.refusals.Reset()
		} else {
			s.refusals.Purge(now)
		}
	})
	if s.admin == nil {
		return
	}
	if err := s.admin.Sync(ctx, now); err != nil {
		s.events.ErrorContext(ctx, "admin-store", "err", logging.ErrorText(err))
	}
	if !s.counts.Enabled() {
		s.refusals.Reset()
		s.visits.Forget()
	}
	s.flushStatistics(ctx, now)
}

func (s *Server) flushStatistics(ctx context.Context, now time.Time) {
	if err := s.counts.Flush(ctx, now); err != nil {
		s.events.ErrorContext(ctx, "admin-counts", "err", logging.ErrorText(err))
	}
}

// RunUsage samples the load of the machine for the admin area until ctx ends.
func (s *Server) RunUsage(ctx context.Context) {
	if s.admin == nil {
		return
	}
	s.usage.Run(ctx, usageInterval)
}
