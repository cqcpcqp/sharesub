package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestPricingPublishTransactionHistoryAndRollback(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	id, err := store.CurrentPricingID(ctx)
	if err != nil || id != 1 {
		t.Fatalf("initial pointer=%d err=%v", id, err)
	}
	initial, err := store.PricingVersion(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	event := domain.AuditEvent{ID: "price-publish", ActorUserID: "admin", Action: "pricing.published", ResourceType: "pricing", CreatedAt: time.Now()}
	input := domain.PublishPricingInput{BaseVersionID: id, Reason: "test", Config: initial.Config}
	input.Config.WebSearchMicros *= 2
	next, err := store.PublishPricing(ctx, input, event)
	if err != nil || next.ID <= initial.ID || next.Config.WebSearchMicros != 20_000 {
		t.Fatalf("publish=%+v err=%v", next, err)
	}
	old, err := store.PricingVersion(ctx, initial.ID)
	if err != nil || old.Config.WebSearchMicros != 10_000 {
		t.Fatalf("old version mutated: %v", err)
	}
	if _, err := store.PublishPricing(ctx, input, event); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale publication=%v", err)
	}
	input.BaseVersionID = next.ID
	event.ID = "bad-audit"
	event.ActorUserID = "missing-user"
	if _, err := store.PublishPricing(ctx, input, event); err == nil {
		t.Fatal("expected audit foreign key failure")
	}
	current, err := store.CurrentPricingID(ctx)
	if err != nil || current != next.ID {
		t.Fatal("failed audit changed active pointer")
	}
	history, err := store.PricingHistory(ctx, 0)
	if err != nil || len(history) != 2 || history[0].ID != next.ID {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	history, err = store.PricingHistory(ctx, next.ID)
	if err != nil || len(history) != 1 || history[0].ID != initial.ID {
		t.Fatalf("history cursor=%+v err=%v", history, err)
	}
	input.Config = initial.Config
	event.ID, event.ActorUserID = "restore-price", "admin"
	restored, err := store.PublishPricing(ctx, input, event)
	if err != nil || restored.ID <= next.ID || restored.Config.WebSearchMicros != 10_000 {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	var audits int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='pricing.published'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}

func TestPricingConcurrentPublishHasOneWinner(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	initial, err := store.PricingVersion(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"publisher-one", "publisher-two"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			_, err := store.PublishPricing(ctx, domain.PublishPricingInput{BaseVersionID: 1, Reason: id, Config: initial.Config}, domain.AuditEvent{ID: id, ActorUserID: "admin", Action: "pricing.published", ResourceType: "pricing", CreatedAt: time.Now()})
			results <- err
		}(id)
	}
	group.Wait()
	close(results)
	var winners, conflicts int
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, domain.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}
