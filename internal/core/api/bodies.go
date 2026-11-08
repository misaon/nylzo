package api

// CreateHeader and PolicyHeader keep pointers for the members whose absence a reader must tell from a zero.
type CreateHeader struct {
	ID          string  `json:"id"`
	IDToken     string  `json:"id_token"`
	Epoch       *uint32 `json:"epoch"`
	ReadKey     string  `json:"read_key"`
	VerifierKey *string `json:"verifier_key"`
	ManageKey   string  `json:"manage_key"`
	KDFID       int     `json:"kdf_id"`
	OpensMax    int     `json:"opens_max"`
	TTLSeconds  int64   `json:"ttl_s"`
	Size        int64   `json:"size"`
}

type PolicyHeader struct {
	OpensMax   *int `json:"opens_max"`
	TTLSeconds *int `json:"ttl_s"`
	KDFID      *int `json:"kdf_id"`
	Flags      *int `json:"flags"`
}

type InstanceLimits struct {
	MaxFileBytes      int64 `json:"max_file_bytes"`
	MaxTextBytes      int64 `json:"max_text_bytes"`
	MaxObjectBytes    int64 `json:"max_object_bytes"`
	MaxTTLSeconds     int64 `json:"max_ttl_s"`
	DefaultTTLSeconds int64 `json:"default_ttl_s"`
	MaxOpens          int   `json:"max_opens"`
}

type Work struct {
	Required   bool   `json:"required"`
	Difficulty int    `json:"difficulty"`
	Challenge  string `json:"challenge,omitempty"`
}

type InstanceCreate struct {
	ID        string  `json:"id"`
	IDToken   string  `json:"id_token"`
	ExpiresAt string  `json:"expires_at"`
	Epoch     *uint32 `json:"epoch"`
	Share     string  `json:"share"`
}

type Challenge struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

type Metadata struct {
	State          string `json:"state"`
	CreatedAt      string `json:"created_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	OpensMax       int    `json:"opens_max"`
	OpensRemaining int    `json:"opens_remaining"`
	TTLSeconds     int64  `json:"ttl_s"`
	KDFID          int    `json:"kdf_id"`
	Size           int64  `json:"size"`
	FirstOpenedAt  string `json:"first_opened_at,omitempty"`
	EndedAt        string `json:"ended_at,omitempty"`
}

type Status struct {
	State                    string `json:"state"`
	OpensUsed                int    `json:"opens_used"`
	OpensMax                 int    `json:"opens_max"`
	CreatedAt                string `json:"created_at,omitempty"`
	ExpiresAt                string `json:"expires_at,omitempty"`
	FirstOpenedAt            string `json:"first_opened_at,omitempty"`
	EndedAt                  string `json:"ended_at,omitempty"`
	FailedPassphraseAttempts int    `json:"failed_passphrase_attempts"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type RateLimitedResponse struct {
	Error      string `json:"error"`
	RetryAfter int64  `json:"retry_after"`
}

type PassphraseResponse struct {
	Error        string `json:"error"`
	RetryAfter   int64  `json:"retry_after"`
	AttemptsLeft int    `json:"attempts_left"`
}

type GoneResponse struct {
	Error string `json:"error"`
	State string `json:"state"`
}
