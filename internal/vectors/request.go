package vectors

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

var errSignatureUnverified = errors.New("does not verify")

type Request struct {
	Components    signing.Components `json:"components"`
	Name          string             `json:"name"`
	Seed          string             `json:"seed_hex"`
	PublicKey     string             `json:"public_key"`
	Purpose       string             `json:"purpose"`
	Method        string             `json:"method"`
	Path          string             `json:"path"`
	Challenge     string             `json:"challenge"`
	Body          string             `json:"body_hex"`
	Message       string             `json:"message_hex"`
	Signature     string             `json:"signature"`
	Authorization string             `json:"authorization,omitempty"`
	Verifier      string             `json:"verifier_header,omitempty"`
}

type requestCase struct {
	name, purpose, method, path string
	body                        []byte
	components                  signing.Components
}

func (f *File) request() error {
	id := deterministicID("request").String()
	messagePath := "/api/v1/messages/" + id
	key, _, keyErr := deterministicShareKey("request-share-key")
	if keyErr != nil {
		return keyErr
	}
	shareKey := share.FormatKey(key.PublicKey())
	lease := core.Encode(deterministic("request-lease", 32))
	cases := []requestCase{
		{"metadata read", signing.PurposeRead, http.MethodGet, messagePath, nil, signing.Components{}},
		{
			"claim verifier",
			signing.PurposeVerifier,
			http.MethodPost,
			messagePath + "/claim",
			nil,
			signing.Components{ShareKey: shareKey},
		},
		{"status manage", signing.PurposeManage, http.MethodGet, messagePath + "/status", nil, signing.Components{}},
		{
			"abuse report with a body",
			signing.PurposeRead,
			http.MethodPost,
			"/api/v1/abuse",
			[]byte(`{"id":"` + id + `","reason":"spam"}`),
			signing.Components{},
		},
		{
			"claim read",
			signing.PurposeRead,
			http.MethodPost,
			messagePath + "/claim",
			nil,
			signing.Components{ShareKey: shareKey},
		},
		{
			"blob read",
			signing.PurposeRead,
			http.MethodGet,
			messagePath + "/blob",
			nil,
			signing.Components{ShareKey: shareKey, Lease: lease},
		},
		{"ack read", signing.PurposeRead, http.MethodPost, messagePath + "/ack", nil, signing.Components{Lease: lease}},
	}
	for i := range cases {
		request, err := requestVector(&cases[i])
		if err != nil {
			return err
		}
		f.Request = append(f.Request, request)
	}
	return nil
}

func requestVector(vector *requestCase) (Request, error) {
	seed := deterministic32("request-seed/" + vector.name)
	challenge := [signing.ChallengeLen]byte(deterministic("request-challenge/"+vector.name, signing.ChallengeLen))
	signature := signing.SignRequest(
		seed,
		vector.purpose,
		vector.method,
		vector.path,
		challenge,
		vector.body,
		vector.components,
	)
	if !signature.Verify(vector.purpose, vector.method, vector.path, vector.body, vector.components) {
		return Request{}, fmt.Errorf("vectors: request signature %q %w", vector.name, errSignatureUnverified)
	}
	message := signing.RequestMessage(
		vector.purpose,
		vector.method,
		vector.path,
		&challenge,
		vector.body,
		vector.components,
	)
	request := Request{
		Components: vector.components,
		Name:       vector.name,
		Seed:       hex.EncodeToString(seed[:]),
		PublicKey:  core.Encode(signature.PublicKey[:]),
		Purpose:    vector.purpose,
		Method:     vector.method,
		Path:       vector.path,
		Challenge:  core.Encode(challenge[:]),
		Body:       hex.EncodeToString(vector.body),
		Message:    hex.EncodeToString(message),
		Signature:  core.Encode(signature.Value[:]),
	}
	if vector.purpose == signing.PurposeVerifier {
		request.Verifier = signature.VerifierHeader()
	} else {
		request.Authorization = signature.Authorization()
	}
	return request, nil
}
