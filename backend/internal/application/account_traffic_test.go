package application

import (
	"testing"
	"time"
)

func TestAccountTrafficControllerEvictsInactiveStates(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	release, err := controller.acquire("old-account", 1, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(controller.states) != 1 {
		t.Fatalf("traffic states = %d, want 1", len(controller.states))
	}
	later := now.Add(accountTrafficStateTTL + accountTrafficCleanupPeriod)
	release, err = controller.acquire("new-account", 1, 0, later)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if controller.states["old-account"] != nil || controller.states["new-account"] == nil {
		t.Fatalf("traffic states were not pruned: %+v", controller.states)
	}
}
