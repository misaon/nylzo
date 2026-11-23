package localclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/misaon/nylzo/internal/core/api"
)

type unreachableInstance struct{}

func (unreachableInstance) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestRelayAnswersUnavailableWhenItCannotForward(t *testing.T) {
	t.Parallel()
	forwarder := relay{instance: &url.URL{Scheme: "https", Host: "nylzo.example"}, transport: unreachableInstance{}}
	for name, method := range map[string]string{"an unreachable instance": http.MethodGet, "a broken method": "GET /"} {
		recorder := httptest.NewRecorder()
		forwarder.ServeHTTP(recorder, &http.Request{
			Method: method,
			URL:    &url.URL{Path: api.VersionPrefix + "messages"},
			Header: http.Header{},
			Body:   http.NoBody,
		})
		if recorder.Code != http.StatusBadGateway || recorder.Body.String() != `{"error":"unavailable"}` {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
}
