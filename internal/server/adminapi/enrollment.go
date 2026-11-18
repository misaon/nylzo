package adminapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/passkey"
)

type enrollmentOptionsRequest struct {
	Token string `json:"token"`
}

type credentialParameter struct {
	Type      string `json:"type"`
	Algorithm int    `json:"alg"`
}

type credentialDescriptor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type relyingPartyEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type userEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type authenticatorSelection struct {
	ResidentKey        string `json:"residentKey"`
	RequireResidentKey bool   `json:"requireResidentKey"`
	UserVerification   string `json:"userVerification"`
}

type creationOptions struct {
	RelyingParty       relyingPartyEntity     `json:"rp"`
	User               userEntity             `json:"user"`
	Challenge          string                 `json:"challenge"`
	Parameters         []credentialParameter  `json:"pubKeyCredParams"`
	Timeout            int64                  `json:"timeout"`
	Attestation        string                 `json:"attestation"`
	Selection          authenticatorSelection `json:"authenticatorSelection"`
	ExcludeCredentials []credentialDescriptor `json:"excludeCredentials"`
}

func (a *API) handleEnrollmentOptions(w http.ResponseWriter, r *http.Request) {
	var request enrollmentOptionsRequest
	if !readJSON(w, r, &request) {
		return
	}
	relyingParty, known := a.relyingParty(r)
	token, ok := decode(request.Token)
	if !known {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	if !ok {
		a.refuseEnrollment(w, r)
		return
	}
	now := time.Now()
	hashed := adminstore.HashToken(token)
	open, err := a.Store.EnrollmentOpen(r.Context(), hashed, now)
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	if !open {
		a.refuseEnrollment(w, r)
		return
	}
	held, err := a.Store.Passkeys(r.Context())
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	challenge := a.sessions.beginEnrollment(hashed, now)
	writeJSON(w, http.StatusOK, ceremonyResponse[creationOptions]{
		PublicKey: newCreationOptions(relyingParty, challenge, held),
	})
}

// Every passkey of an instance shares one user handle, so an authenticator shows one account for the instance;
// it refuses to add a second passkey, because the passkeys the instance holds are excluded.
func newCreationOptions(
	relyingParty passkey.RelyingParty,
	challenge []byte,
	held []adminstore.Passkey,
) creationOptions {
	handle := sha256.Sum256([]byte(userHandleLabel + relyingParty.ID))
	options := creationOptions{
		RelyingParty: relyingPartyEntity{ID: relyingParty.ID, Name: relyingPartyName},
		User: userEntity{
			ID:          core.Encode(handle[:userHandleLength]),
			Name:        operatorName,
			DisplayName: operatorTitle,
		},
		Challenge:   core.Encode(challenge),
		Timeout:     ceremonyTimeoutMs,
		Attestation: "none",
		Selection: authenticatorSelection{
			ResidentKey:        residentKeyRequire,
			RequireResidentKey: true,
			UserVerification:   userVerification,
		},
		ExcludeCredentials: []credentialDescriptor{},
	}
	for _, algorithm := range passkey.Algorithms() {
		options.Parameters = append(
			options.Parameters,
			credentialParameter{Type: publicKeyType, Algorithm: int(algorithm)},
		)
	}
	for index := range held {
		options.ExcludeCredentials = append(
			options.ExcludeCredentials,
			credentialDescriptor{Type: publicKeyType, ID: core.Encode(held[index].ID)},
		)
	}
	return options
}

type enrollmentRequest struct {
	Token             string `json:"token"`
	CredentialID      string `json:"credential_id"`
	ClientData        string `json:"client_data"`
	AuthenticatorData string `json:"authenticator_data"`
	PublicKey         string `json:"public_key"`
	Algorithm         int    `json:"algorithm"`
}

func (a *API) handleEnrollment(w http.ResponseWriter, r *http.Request) {
	var request enrollmentRequest
	if !readJSON(w, r, &request) {
		return
	}
	registration, token, complete := decodeRegistration(&request)
	relyingParty, known := a.relyingParty(r)
	if !known {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	if !complete {
		a.refuseEnrollment(w, r)
		return
	}
	now := time.Now()
	hashed := adminstore.HashToken(token)
	credential, verified := a.verifyRegistration(relyingParty, registration, hashed, now)
	if !verified {
		a.refuseEnrollment(w, r)
		return
	}
	err := a.Store.Enroll(r.Context(), hashed, &credential, now)
	if errors.Is(err, adminstore.ErrEnrollmentClosed) || errors.Is(err, adminstore.ErrTooManyPasskeys) ||
		errors.Is(err, adminstore.ErrPasskeyEnrolled) {
		a.refuseEnrollment(w, r)
		return
	}
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	if syncErr := a.Sync(context.WithoutCancel(r.Context()), now); syncErr != nil {
		a.Events.ErrorContext(r.Context(), "admin-store", "err", logging.ErrorText(syncErr))
	}
	a.Events.InfoContext(r.Context(), "admin-passkey-enrolled")
	writeJSON(w, http.StatusCreated, stateResponse{Enrolled: true})
}

func (a *API) verifyRegistration(
	relyingParty passkey.RelyingParty,
	registration *passkey.Registration,
	token adminstore.TokenHash,
	now time.Time,
) (passkey.Credential, bool) {
	challenge, err := passkey.ChallengeOf(registration.ClientDataJSON)
	if err != nil {
		return passkey.Credential{}, false
	}
	started, ok := a.sessions.finishEnrollment(challenge, now)
	if !ok || !sameToken(started.token, token) {
		return passkey.Credential{}, false
	}
	credential, err := relyingParty.VerifyRegistration(registration, challenge)
	return credential, err == nil
}

func decodeRegistration(request *enrollmentRequest) (*passkey.Registration, []byte, bool) {
	var (
		registration passkey.Registration
		complete     [5]bool
		token        []byte
	)
	token, complete[0] = decode(request.Token)
	registration.CredentialID, complete[1] = decode(request.CredentialID)
	registration.ClientDataJSON, complete[2] = decode(request.ClientData)
	registration.AuthenticatorData, complete[3] = decode(request.AuthenticatorData)
	registration.PublicKey, complete[4] = decode(request.PublicKey)
	registration.Algorithm = passkey.Algorithm(request.Algorithm)
	return &registration, token, complete == [5]bool{true, true, true, true, true}
}
