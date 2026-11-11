package storetest

import (
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

// resealWrapsVisitsEveryRow checks that ResealWraps hands over every row, past its first batch, and stores the wraps
// that the callback changed.
func (f *fixture) resealWrapsVisitsEveryRow() {
	t := f.t
	created := make([]*message, 0, 2*store.SweepBatch+1)
	for range cap(created) {
		created = append(created, f.createRow(1, oneHour, false))
	}
	next := seal.NewKey()
	visited := f.reseal(next, []seal.Key{recordsKey})
	for _, message := range created {
		if visited[message.Row.Locator] != 1 {
			t.Fatalf("a row was handed over %d times", visited[message.Row.Locator])
		}
		if !f.opens(message, next) || f.opens(message, recordsKey) {
			t.Fatal("a resealed row must open under the new pepper key and only under it")
		}
	}
	for locator, changes := range f.reseal(next, nil) {
		if changes != 0 {
			t.Fatalf("the row %x under the current pepper key was resealed again", locator[:4])
		}
	}
}

// reseal counts the visits to each row, or the changes when no earlier key is given.
func (f *fixture) reseal(current seal.Key, earlier []seal.Key) map[seal.Locator]int {
	counts := make(map[seal.Locator]int)
	err := f.store.ResealWraps(f.t.Context(), func(row *store.Row) bool {
		changed, err := store.ResealRow(row, current, earlier)
		if err != nil {
			f.t.Errorf("a row of this store does not reseal: %v", err)
		}
		if earlier != nil || changed {
			counts[row.Locator]++
		}
		return changed
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return counts
}

func (f *fixture) opens(created *message, pepperKey seal.Key) bool {
	row, err := f.store.Load(f.t.Context(), created.Row.Locator)
	if err != nil {
		f.t.Fatal(err)
	}
	_, readErr := seal.UnwrapRecordKey(pepperKey, created.readKey, row.ReadWrap, row.Locator, seal.Read)
	_, manageErr := seal.UnwrapRecordKey(pepperKey, created.manageKey, row.ManageWrap, row.Locator, seal.Manage)
	return readErr == nil && manageErr == nil
}
