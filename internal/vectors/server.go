package vectors

import (
	"encoding/hex"

	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
)

type Server struct {
	Pepper           string            `json:"pepper_hex"`
	PepperID         string            `json:"pepper_id_hex"`
	PreviousPepper   string            `json:"previous_pepper_hex"`
	PreviousPepperID string            `json:"previous_pepper_id_hex"`
	MACs             []MACCase         `json:"macs"`
	MinuteEpochs     []MinuteEpochCase `json:"minute_epochs"`
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
	pepper.Manage,
	pepper.Lease,
	pepper.PoW,
	pepper.IDToken,
	pepper.Challenge,
	pepper.OPAQUEServer,
}

func (f *File) server() {
	current := deterministic("pepper/current", 32)
	previous := deterministic("pepper/previous", 32)
	ring := must(pepper.NewRing(current, previous))
	previousRing := must(pepper.NewRing(previous, nil))
	currentID, previousID := ring.CurrentID(), previousRing.CurrentID()
	f.Server = Server{
		Pepper:           hex.EncodeToString(current),
		PepperID:         hex.EncodeToString(currentID[:]),
		PreviousPepper:   hex.EncodeToString(previous),
		PreviousPepperID: hex.EncodeToString(previousID[:]),
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
	epochID := deterministicID("epoch")
	locator := seal.LocatorOf(epochID[:])
	f.Server.MinuteEpochs = minuteEpochs(epoch.Seed(*deterministic32("epoch/minute-seed")), locator)
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
