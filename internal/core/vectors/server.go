package vectors

import (
	"encoding/hex"

	"github.com/misaon/nylzo/internal/pepper"
)

type Server struct {
	Pepper           string    `json:"pepper_hex"`
	PepperID         int       `json:"pepper_id"`
	PreviousPepper   string    `json:"previous_pepper_hex"`
	PreviousPepperID int       `json:"previous_pepper_id"`
	MACs             []MACCase `json:"macs"`
}

type MACCase struct {
	Purpose string `json:"purpose"`
	Input   string `json:"input_hex"`
	MAC     string `json:"mac_hex"`
}

var purposes = []pepper.Purpose{
	pepper.Read,
	pepper.Verifier,
	pepper.Manage,
	pepper.Lease,
	pepper.PoW,
	pepper.IDToken,
	pepper.Challenge,
}

func (f *File) server() error {
	current := deterministic("pepper/current", 32)
	previous := deterministic("pepper/previous", 32)
	ring, err := pepper.NewRing(current, previous)
	if err != nil {
		return err
	}
	previousRing, err := pepper.NewRing(previous, nil)
	if err != nil {
		return err
	}
	f.Server = Server{
		Pepper:           hex.EncodeToString(current),
		PepperID:         int(ring.CurrentID()),
		PreviousPepper:   hex.EncodeToString(previous),
		PreviousPepperID: int(previousRing.CurrentID()),
	}
	for _, purpose := range purposes {
		input := deterministic("mac-input/"+string(purpose), 32)
		mac, _ := ring.MAC(purpose, input)
		f.Server.MACs = append(f.Server.MACs, MACCase{
			Purpose: string(purpose),
			Input:   hex.EncodeToString(input),
			MAC:     hex.EncodeToString(mac[:]),
		})
	}
	return nil
}
