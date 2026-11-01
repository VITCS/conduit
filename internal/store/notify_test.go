package store

import (
	"context"
	"os"
	"testing"
	"time"

	"conduit/internal/model"
)

// TestPostgresEventRelay verifies that an event published on one Pg instance is
// delivered to a subscriber on a second instance via LISTEN/NOTIFY. Skipped
// unless CONDUIT_TEST_POSTGRES names a database.
func TestPostgresEventRelay(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CONDUIT_TEST_POSTGRES to run the LISTEN/NOTIFY relay test")
	}
	pgA, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("pgA: %v", err)
	}
	defer pgA.Close()
	pgB, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("pgB: %v", err)
	}
	defer pgB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pgA.StartEventRelay(ctx) // forwards A's publishes via NOTIFY
	pgB.StartEventRelay(ctx) // listens and delivers locally

	// Subscribe on B.
	id, ch := pgB.Broker().Subscribe()
	defer pgB.Broker().Unsubscribe(id)

	// Give the LISTEN connections a moment to establish.
	time.Sleep(500 * time.Millisecond)

	// Publish on A.
	pgA.Broker().Publish(model.Event{Type: "step.updated", RunID: "r-relay", NodeID: "n1", Status: model.StatusSuccess})

	select {
	case ev := <-ch:
		if ev.RunID != "r-relay" || ev.Type != "step.updated" {
			t.Fatalf("unexpected relayed event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event was not relayed across instances within 5s")
	}
}
