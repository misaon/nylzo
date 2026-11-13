package api

import (
	"slices"
	"time"
)

// IDTokenLifetime is how long an id token stays valid after the instance response that issued it (spec section 5.4).
const IDTokenLifetime = 10 * time.Minute

const (
	Prefix           = "/api/"
	VersionPrefix    = Prefix + "v1/"
	InstancePath     = VersionPrefix + "instance"
	ChallengePath    = VersionPrefix + "challenge"
	MessagesPath     = VersionPrefix + "messages"
	AbusePath        = VersionPrefix + "abuse"
	TransparencyPath = "/.well-known/nylzo/transparency.json"
	HealthPath       = "/healthz"
)

const (
	ClaimAction  = "/claim"
	BlobAction   = "/blob"
	AckAction    = "/ack"
	BurnAction   = "/burn"
	CancelAction = "/cancel"
	StatusAction = "/status"
)

const (
	CreateHeaderName   = "X-Nylzo-Create"
	WorkHeaderName     = "X-Nylzo-Pow"
	TTLHeaderName      = "X-Nylzo-Ttl"
	ShareKeyHeaderName = "X-Nylzo-Share-Key"
	ShareHeaderName    = "X-Nylzo-Share"
	LeaseHeaderName    = "X-Nylzo-Lease"
	PolicyHeaderName   = "X-Nylzo-Policy"
	VerifierHeaderName = "X-Nylzo-Verifier"
	// FlagsHeaderName names the envelope flags a reader opens, on every claim and download (spec 6.4).
	FlagsHeaderName = "X-Nylzo-Flags"
)

const (
	ErrorBadRequest     = "bad_request"
	ErrorClientOutdated = "client_outdated"
	ErrorContact        = "contact"
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
)

const (
	StateAvailable = "available"
	StateOpened    = "opened"
	StateExpired   = "expired"
	StateCancelled = "cancelled"
	StateDeleted   = "deleted"
)

func MessagePath(id string) string {
	return MessagesPath + "/" + id
}

func KnownState(state string) bool {
	return slices.Contains([]string{StateAvailable, StateOpened, StateExpired, StateCancelled, StateDeleted}, state)
}
