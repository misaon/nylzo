package adminapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/server/visits"
)

const databaseFile = "nylzo.db"

// timeline lists the metrics the dashboard draws over time.
var timeline = []string{
	visits.MetricPeople, visits.MetricRobots, visits.MetricUnconfirmed, visits.MetricVisitors,
	statistics.MetricCreated, statistics.MetricOpened, statistics.MetricEnded, statistics.MetricExpired,
	statistics.MetricRefusals,
}

type span struct {
	resolution adminstore.Resolution
	periods    int
	// compared tells whether the store keeps the span before this one, which the trend compares with.
	compared bool
}

var spans = map[string]span{
	"24h": {adminstore.Hourly, 24, true},
	"7d":  {adminstore.Hourly, 7 * 24, true},
	"30d": {adminstore.Daily, 30, true},
	"90d": {adminstore.Daily, 90, true},
	"12m": {adminstore.Daily, 365, false},
}

type overviewResponse struct {
	Challenge         string             `json:"challenge,omitempty"`
	Range             string             `json:"range"`
	From              int64              `json:"from"`
	ResolutionSeconds int64              `json:"resolution_s"`
	Series            map[string][]int64 `json:"series"`
	Totals            map[string]int64   `json:"totals"`
	Previous          map[string]int64   `json:"previous,omitempty"`
	Countries         []statistics.Share `json:"countries"`
	Pages             []statistics.Share `json:"pages"`
	Robots            []statistics.Share `json:"robots"`
	Stored            int                `json:"stored"`
	Storage           storageResponse    `json:"storage"`
	Security          securityResponse   `json:"security"`
	Source            sourceResponse     `json:"source"`
	Version           string             `json:"version"`
}

type storageResponse struct {
	FreeBytes     uint64 `json:"free_bytes"`
	TotalBytes    uint64 `json:"total_bytes"`
	ReserveBytes  int64  `json:"reserve_bytes"`
	DatabaseBytes int64  `json:"database_bytes"`
}

type securityResponse struct {
	Passkeys         int   `json:"passkeys"`
	LastSignIn       int64 `json:"last_sign_in"`
	SessionExpiresAt int64 `json:"session_expires_at"`
}

type sourceResponse struct {
	Countries      string `json:"countries"`
	Attribution    string `json:"attribution"`
	AttributionURL string `json:"attribution_url"`
}

func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("range")
	chosen, ok := spans[name]
	if !ok {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	now := time.Now()
	length := time.Duration(chosen.periods) * time.Duration(chosen.resolution) * time.Second
	until := chosen.resolution.Start(now).Add(time.Duration(chosen.resolution) * time.Second)
	from := until.Add(-length)
	current, err := a.counted(r.Context(), chosen.resolution, from, until)
	var earlier spanCounts
	if err == nil && chosen.compared {
		earlier, err = a.counted(r.Context(), chosen.resolution, from.Add(-length), from)
	}
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	response := overviewResponse{
		Challenge:         a.nextChallenge(now),
		Range:             name,
		From:              from.Unix(),
		ResolutionSeconds: int64(chosen.resolution),
		Series:            make(map[string][]int64, len(timeline)),
		Totals:            totals(current),
		Countries:         current.days.Breakdown(visits.CountryPrefix),
		Pages:             current.periods.Breakdown(visits.PagePrefix),
		Robots:            current.periods.Breakdown(visits.RobotPrefix),
		Storage:           a.storage(),
		Version:           a.Version,
	}
	for _, metric := range timeline {
		response.Series[metric] = valuesOf(current.periods, metric)
	}
	if chosen.compared {
		response.Previous = totals(elapsedPart(earlier, now))
	}
	signedIn := sessionOf(r)
	if err = a.describe(r.Context(), &response, &signedIn); err != nil {
		a.writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) describe(ctx context.Context, response *overviewResponse, signedIn *signedSession) error {
	stored, err := a.Stored(ctx)
	if err != nil {
		return err
	}
	held, err := a.Store.Passkeys(ctx)
	if err != nil {
		return err
	}
	response.Stored = stored
	response.Security = securityResponse{Passkeys: len(held), SessionExpiresAt: signedIn.expiresAt.Unix()}
	if !signedIn.previous.IsZero() {
		response.Security.LastSignIn = signedIn.previous.Unix()
	}
	source := a.Countries.Source()
	response.Source = sourceResponse{
		Countries:      source.Name + " " + source.Month,
		Attribution:    source.Attribution,
		AttributionURL: source.AttributionURL,
	}
	return nil
}

func (a *API) storage() storageResponse {
	response := storageResponse{ReserveBytes: a.ReserveBytes}
	if disk, err := hostusage.DiskOf(a.DataDirectory); err == nil {
		response.FreeBytes, response.TotalBytes = disk.FreeBytes, disk.TotalBytes
	}
	if info, err := os.Stat(filepath.Join(a.DataDirectory, databaseFile)); err == nil {
		response.DatabaseBytes = info.Size()
	}
	return response
}

// elapsedPart cuts the span before down to what the running span has had: its last period, which matches the running
// one, counts in proportion to the part of the running period that has passed, so a trend never compares a part with a
// whole.
func elapsedPart(earlier spanCounts, now time.Time) spanCounts {
	scaleLastPeriod(earlier.periods, now)
	if earlier.days != earlier.periods {
		scaleLastPeriod(earlier.days, now)
	}
	return earlier
}

func scaleLastPeriod(series *statistics.Series, now time.Time) {
	passed := now.Unix() - series.Resolution.Start(now).Unix()
	for _, values := range series.Values {
		if last := len(values) - 1; last >= 0 {
			values[last] = (values[last]*passed + int64(series.Resolution)/2) / int64(series.Resolution)
		}
	}
}

// spanCounts holds the counts of a span by its periods and by the UTC days that begin within it, so a span and the
// one before it share no day.
type spanCounts struct {
	periods, days *statistics.Series
}

func (a *API) counted(
	ctx context.Context,
	resolution adminstore.Resolution,
	from, until time.Time,
) (spanCounts, error) {
	periods, err := a.Counts.Read(ctx, resolution, from, until)
	if err != nil || resolution == adminstore.Daily {
		return spanCounts{periods: periods, days: periods}, err
	}
	firstDay := adminstore.Daily.Start(from.Add(time.Duration(adminstore.Daily)*time.Second - time.Second))
	days, err := a.Counts.Read(ctx, adminstore.Daily, firstDay, until)
	return spanCounts{periods: periods, days: days}, err
}

// totals sums each metric over the span as valuesOf shows it, so a chart and its total agree; a metric that is kept
// by the day only is summed over the days that begin within the span.
func totals(counted spanCounts) map[string]int64 {
	summed := make(map[string]int64, len(timeline))
	for _, metric := range timeline {
		source := counted.periods
		if statistics.DailyOnly(metric) {
			source = counted.days
		}
		summed[metric] = 0
		for _, value := range source.Values[metric] {
			summed[metric] += max(0, value)
		}
	}
	return summed
}

// valuesOf gives a value for every period. A late sign of a person takes its view back out of the unconfirmed
// ones, in an hour that may hold none, so a period never shows less than nothing.
func valuesOf(series *statistics.Series, metric string) []int64 {
	values := make([]int64, series.Periods)
	for index, value := range series.Values[metric] {
		values[index] = max(0, value)
	}
	return values
}

func withSession(r *http.Request, signedIn *signedSession) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, *signedIn))
}

func sessionOf(r *http.Request) signedSession {
	signedIn, _ := r.Context().Value(sessionContextKey{}).(signedSession)
	return signedIn
}
