package vectors

import (
	"encoding/hex"

	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/epoch"
)

type Server struct {
	Pepper           string      `json:"pepper_hex"`
	PepperID         int         `json:"pepper_id"`
	PreviousPepper   string      `json:"previous_pepper_hex"`
	PreviousPepperID int         `json:"previous_pepper_id"`
	MACs             []MACCase   `json:"macs"`
	Epochs           []EpochCase `json:"epochs"`
}

// EpochCase is one hour of the share ratchet of spec section 5.6, starting from a fixed seed.
type EpochCase struct {
	Hour     uint32 `json:"hour"`
	Seed     string `json:"seed_hex"`
	EpochKey string `json:"epoch_key_hex"`
	Locator  string `json:"locator_hex"`
	Share    string `json:"e_hex"`
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
		mac := ring.MAC(purpose, input).MAC
		f.Server.MACs = append(f.Server.MACs, MACCase{
			Purpose: string(purpose),
			Input:   hex.EncodeToString(input),
			MAC:     hex.EncodeToString(mac[:]),
		})
	}
	seed := epoch.Seed(*deterministic32("epoch/seed"))
	epochID := deterministicID("epoch")
	locator := seal.LocatorOf(epochID[:])
	const firstHour = 493_000
	for hour := uint32(firstHour); hour < firstHour+3; hour++ {
		key := epoch.Key(&seed)
		share := epoch.ShareOf(&key, locator)
		f.Server.Epochs = append(f.Server.Epochs, EpochCase{
			Hour:     hour,
			Seed:     hex.EncodeToString(seed[:]),
			EpochKey: hex.EncodeToString(key[:]),
			Locator:  hex.EncodeToString(locator[:]),
			Share:    hex.EncodeToString(share[:]),
		})
		seed = epoch.Next(&seed)
	}
	return nil
}
