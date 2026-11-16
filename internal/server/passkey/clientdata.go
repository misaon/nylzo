package passkey

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
)

const (
	ceremonyCreate = "webauthn.create"
	ceremonyGet    = "webauthn.get"

	minimumChallengeSize  = 16
	maximumClientDataSize = 4096
)

type clientData struct {
	Type        string          `json:"type"`
	Challenge   string          `json:"challenge"`
	Origin      string          `json:"origin"`
	CrossOrigin json.RawMessage `json:"crossOrigin"`
	TopOrigin   json.RawMessage `json:"topOrigin"`
}

func ChallengeOf(clientDataJSON []byte) ([]byte, error) {
	data, err := decodeClientData(clientDataJSON)
	if err != nil {
		return nil, err
	}
	challenge, err := base64.RawURLEncoding.Strict().DecodeString(data.Challenge)
	if err != nil {
		return nil, refused("the challenge is not base64url")
	}
	return challenge, nil
}

func decodeClientData(clientDataJSON []byte) (clientData, error) {
	if len(clientDataJSON) > maximumClientDataSize {
		return clientData{}, refused("the client data is too large")
	}
	var data clientData
	if err := json.Unmarshal(clientDataJSON, &data); err != nil {
		return clientData{}, refused("the client data is not a JSON object")
	}
	return data, nil
}

func (rp RelyingParty) verifyClientData(clientDataJSON []byte, ceremony string, challenge []byte) error {
	if rp.ID == "" || rp.Origin == "" {
		return refused("the relying party is not configured")
	}
	if len(challenge) < minimumChallengeSize {
		return refused("the expected challenge is too short")
	}
	data, err := decodeClientData(clientDataJSON)
	if err != nil {
		return err
	}
	if data.Type != ceremony {
		return refused("the client data has the wrong type")
	}
	expectedChallenge := base64.RawURLEncoding.EncodeToString(challenge)
	if subtle.ConstantTimeCompare([]byte(data.Challenge), []byte(expectedChallenge)) != 1 {
		return refused("the challenge does not match")
	}
	if data.Origin != rp.Origin {
		return refused("the origin does not match")
	}
	if data.CrossOrigin != nil && string(data.CrossOrigin) != "false" {
		return refused("the ceremony ran in a cross-origin frame")
	}
	if data.TopOrigin != nil {
		return refused("the ceremony ran in a frame")
	}
	return nil
}
