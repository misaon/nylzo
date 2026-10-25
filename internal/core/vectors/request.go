package vectors

import (
	"encoding/hex"
	"fmt"

	"github.com/misaon/nylzo/internal/core"
)

type Request struct {
	Name          string `json:"name"`
	Seed          string `json:"seed_hex"`
	PublicKey     string `json:"public_key"`
	Purpose       string `json:"purpose"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	Challenge     string `json:"challenge"`
	Body          string `json:"body_hex"`
	Message       string `json:"message_hex"`
	Signature     string `json:"signature"`
	Authorization string `json:"authorization,omitempty"`
	Verifier      string `json:"verifier_header,omitempty"`
}

type requestCase struct {
	name, purpose, method, path string
	body                        []byte
}

func (f *File) request() error {
	id := deterministicID("request").String()
	messagePath := "/api/v1/messages/" + id
	cases := []requestCase{
		{"metadata read", core.PurposeRead, "GET", messagePath, nil},
		{"claim verifier", core.PurposeVerifier, "POST", messagePath + "/claim", nil},
		{"status manage", core.PurposeManage, "GET", messagePath + "/status", nil},
		{
			"abuse report with a body",
			core.PurposeRead,
			"POST",
			"/api/v1/abuse",
			[]byte(`{"id":"` + id + `","reason":"spam"}`),
		},
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
	challenge := [core.ChallengeLen]byte(deterministic("request-challenge/"+vector.name, core.ChallengeLen))
	signature := core.SignRequest(seed, vector.purpose, vector.method, vector.path, challenge, vector.body)
	if !signature.Verify(vector.purpose, vector.method, vector.path, vector.body) {
		return Request{}, fmt.Errorf("vectors: request signature %q does not verify", vector.name)
	}
	message := core.RequestMessage(vector.purpose, vector.method, vector.path, &challenge, vector.body)
	request := Request{
		Name:      vector.name,
		Seed:      hex.EncodeToString(seed[:]),
		PublicKey: core.Encode(signature.PublicKey[:]),
		Purpose:   vector.purpose,
		Method:    vector.method,
		Path:      vector.path,
		Challenge: core.Encode(challenge[:]),
		Body:      hex.EncodeToString(vector.body),
		Message:   hex.EncodeToString(message),
		Signature: core.Encode(signature.Sig[:]),
	}
	if vector.purpose == core.PurposeVerifier {
		request.Verifier = signature.VerifierHeader()
	} else {
		request.Authorization = signature.Authorization()
	}
	return request, nil
}
