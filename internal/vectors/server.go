package vectors

import (
	"encoding/hex"

	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
)

type Server struct {
	Pepper           string            `json:"pepper_hex"`
	PepperID         int               `json:"pepper_id"`
	PreviousPepper   string            `json:"previous_pepper_hex"`
	PreviousPepperID int               `json:"previous_pepper_id"`
	MACs             []MACCase         `json:"macs"`
	Epochs           []EpochCase       `json:"epochs"`
	MinuteEpochs     []MinuteEpochCase `json:"minute_epochs"`
}

// EpochCase is one hour of the chain of hours that releases before Nylzo 0.14.0 made shares with (spec section 5.6).
type EpochCase struct {
	Hour     uint32 `json:"hour"`
	Seed     string `json:"seed_hex"`
	EpochKey string `json:"epoch_key_hex"`
	Locator  string `json:"locator_hex"`
	Share    string `json:"e_hex"`
}

// MinuteEpochCase is one minute of a generation that starts in the hour of the first case from a fixed seed: the
// seed of its hour, the seed of the minute, the epoch key and the share (spec section 5.6).
type MinuteEpochCase struct {
	Minute     uint32 `json:"minute"`
	HourSeed   string `json:"hour_seed_hex"`
	MinuteSeed string `json:"minute_seed_hex"`
	EpochKey   string `json:"epoch_key_hex"`
	Locator    string `json:"locator_hex"`
	Share      string `json:"e_hex"`
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
	f.Server.MinuteEpochs = minuteEpochs(epoch.Seed(*deterministic32("epoch/minute-seed")), locator)
	return nil
}

// minuteEpochs walks the first two minutes of an hour, its last minute and the first minutes of the next hour.
func minuteEpochs(hourSeed epoch.Seed, locator seal.Locator) []MinuteEpochCase {
	const firstMinute = 493_000 * 60
	var cases []MinuteEpochCase
	minuteSeed := epoch.FirstMinute(&hourSeed)
	for minute := uint32(firstMinute); minute < firstMinute+62; minute++ {
		if minute%60 == 0 && minute != firstMinute {
			hourSeed = epoch.Next(&hourSeed)
			minuteSeed = epoch.FirstMinute(&hourSeed)
		}
		if minute%60 <= 1 || minute%60 == 59 {
			key := epoch.MinuteKey(&minuteSeed)
			share := epoch.ShareOf(&key, locator)
			cases = append(cases, MinuteEpochCase{
				Minute:     minute,
				HourSeed:   hex.EncodeToString(hourSeed[:]),
				MinuteSeed: hex.EncodeToString(minuteSeed[:]),
				EpochKey:   hex.EncodeToString(key[:]),
				Locator:    hex.EncodeToString(locator[:]),
				Share:      hex.EncodeToString(share[:]),
			})
		}
		minuteSeed = epoch.NextMinute(&minuteSeed)
	}
	return cases
}
