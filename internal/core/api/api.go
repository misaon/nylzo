package api

import (
	"slices"
	"time"
)

// IDTokenLifetime is how long an id token stays valid after the instance response that issued it (spec section 5.4).
const IDTokenLifetime = 10 * time.Minute

const (
	Prefix        = "/api/"
	VersionPrefix = Prefix + "v1/"
	InstancePath  = VersionPrefix + "instance"
	ChallengePath = VersionPrefix + "challenge"
	MessagesPath  = VersionPrefix + "messages"
	VisitsPath    = VersionPrefix + "visits"
	// DocumentVisitsPath takes the reports of the edge about the pages it served; the edge keeps clients off it.
	DocumentVisitsPath = VisitsPath + "/documents"
	AdminPath          = VersionPrefix + "admin"
	TransparencyPath   = "/.well-known/nylzo/transparency.json"
	HealthPath         = "/healthz"
)

const (
	HandshakeAction = "/handshake"
	ClaimAction     = "/claim"
	BlobAction      = "/blob"
	AckAction       = "/ack"
	BurnAction      = "/burn"
	CancelAction    = "/cancel"
	StatusAction    = "/status"
)

const (
	CreateHeaderName       = "X-Nylzo-Create"
	WorkHeaderName         = "X-Nylzo-Pow"
	TTLHeaderName          = "X-Nylzo-Ttl"
	ShareKeyHeaderName     = "X-Nylzo-Share-Key"
	ShareHeaderName        = "X-Nylzo-Share"
	LeaseHeaderName        = "X-Nylzo-Lease"
	PolicyHeaderName       = "X-Nylzo-Policy"
	RegistrationHeaderName = "X-Nylzo-Registration"
	KE1HeaderName          = "X-Nylzo-KE1"
	KE3HeaderName          = "X-Nylzo-KE3"
	// VerifierHeaderName comes only from a client of protocol v2, which the server answers with client_outdated.
	VerifierHeaderName = "X-Nylzo-Verifier"
)

const (
	ErrorBadRequest     = "bad_request"
	ErrorClientOutdated = "client_outdated"
	ErrorNotFound       = "not_found"
	ErrorConflict       = "conflict"
	ErrorShareLost      = "share_lost"
	ErrorLengthRequired = "length_required"
	ErrorTooLarge       = "too_large"
	ErrorUnavailable    = "unavailable"
	ErrorWork           = "pow"
	ErrorIDToken        = "id_token"
	ErrorCrossOrigin    = "cross_origin"
	ErrorInternal       = "internal"
	ErrorGone           = "gone"
	ErrorRateLimited    = "rate_limited"
	ErrorPassphrase     = "passphrase"
	ErrorUnauthorized   = "unauthorized"
)

// DocumentReportsPerSecond and DocumentReportsBurst are the one budget of the reports about served pages: the
// frontend sends no more, and the server counts no more.
const (
	DocumentReportsPerSecond = 50
	DocumentReportsBurst     = 200
)

// A visit signal tells what was seen of a page: the edge reports the document it served, the page itself reports
// the first sign of a person or that a program drives the browser.
const (
	VisitDocument   = "document"
	VisitPerson     = "person"
	VisitAutomation = "automation"
)

const (
	PageHome         = "home"
	PageSent         = "sent"
	PageTransparency = "transparency"
	PageTerms        = "terms"
	PageAgents       = "agents"
	PageOther        = "other"
)

// VisitPages are the pages whose visits are counted; a page that shows a message is never one of them.
var VisitPages = []string{PageHome, PageSent, PageTransparency, PageTerms, PageAgents, PageOther}

const (
	StateAvailable = "available"
	StateOpened    = "opened"
	StateExpired   = "expired"
	StateCancelled = "cancelled"
	StateDeleted   = "deleted"
	StateLost      = "lost"
)

func MessagePath(id string) string {
	return MessagesPath + "/" + id
}

var knownStates = []string{StateAvailable, StateOpened, StateExpired, StateCancelled, StateDeleted, StateLost}

func KnownState(state string) bool {
	return slices.Contains(knownStates, state)
}
