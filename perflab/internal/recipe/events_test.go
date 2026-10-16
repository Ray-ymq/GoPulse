package recipe

import "testing"

func TestOutboxFactsMaterializeTheContractCounts(t *testing.T) {
	counts := map[string]uint64{}
	outbox := uint64(0)
	if err := forEachOutboxEvent(Seed, func(event outboxEvent) error {
		if err := event.validate(); err != nil {
			return err
		}
		outbox++
		counts[event.Type]++
		return nil
	}); err != nil {
		t.Fatalf("iterate outbox facts: %v", err)
	}
	if outbox != ExpectedCounts().Outbox {
		t.Fatalf("outbox=%d want %d", outbox, ExpectedCounts().Outbox)
	}
	want := map[string]uint64{
		"post.created": 50000, "comment.created": 100000,
		"post.liked": 200000, "user.followed": 200000,
	}
	if len(counts) != len(want) {
		t.Fatalf("unexpected event types: %v", counts)
	}
	for eventType, count := range want {
		if counts[eventType] != count {
			t.Fatalf("event type %s count=%d want %d", eventType, counts[eventType], count)
		}
	}
}

func TestNotificationsTraceBackToPublishedOutboxEvents(t *testing.T) {
	outboxEvents := map[string]outboxEvent{}
	if err := forEachOutboxEvent(Seed, func(event outboxEvent) error {
		if _, exists := outboxEvents[event.EventID]; exists {
			t.Fatalf("duplicate outbox event id %s", event.EventID)
		}
		outboxEvents[event.EventID] = event
		return nil
	}); err != nil {
		t.Fatalf("iterate outbox facts: %v", err)
	}

	notifications := uint64(0)
	seen := map[string]bool{}
	if err := forEachNotification(Seed, func(fact notificationFact) error {
		notifications++
		if seen[fact.SourceEventID] {
			t.Fatalf("duplicate notification source event %s", fact.SourceEventID)
		}
		seen[fact.SourceEventID] = true
		event, ok := outboxEvents[fact.SourceEventID]
		if !ok {
			t.Fatalf("notification %s has no originating outbox event", fact.SourceEventID)
		}
		if event.Type == "post.created" {
			t.Fatalf("post.created must never notify: %s", fact.SourceEventID)
		}
		if fact.Type != event.Type || fact.ActorID != event.ActorID || fact.RecipientID != event.RecipientID {
			t.Fatalf("notification %s does not mirror its outbox envelope", fact.SourceEventID)
		}
		if fact.ActorID == 0 || fact.RecipientID == 0 {
			t.Fatalf("notification %s has an empty party", fact.SourceEventID)
		}
		if fact.ActorID == fact.RecipientID {
			t.Fatalf("notification %s is self-directed", fact.SourceEventID)
		}
		if !fact.CreatedAt.Equal(event.OccurredAt.UTC()) {
			t.Fatalf("notification %s has a mismatched creation time", fact.SourceEventID)
		}
		switch fact.Type {
		case "comment.created":
			if fact.PostID != event.PostID || fact.CommentID != event.CommentID || fact.CommentID == 0 {
				t.Fatalf("comment notification %s has an invalid resource shape", fact.SourceEventID)
			}
		case "post.liked":
			if fact.PostID != event.PostID || fact.PostID == 0 || fact.CommentID != 0 {
				t.Fatalf("like notification %s has an invalid resource shape", fact.SourceEventID)
			}
		case "user.followed":
			if fact.PostID != 0 || fact.CommentID != 0 {
				t.Fatalf("follow notification %s must not reference a post or comment", fact.SourceEventID)
			}
		default:
			t.Fatalf("notification %s has an unsupported type %s", fact.SourceEventID, fact.Type)
		}
		return nil
	}); err != nil {
		t.Fatalf("iterate notification facts: %v", err)
	}
	if notifications != ExpectedCounts().Notifications {
		t.Fatalf("notifications=%d want %d", notifications, ExpectedCounts().Notifications)
	}
	// Only post.created events are projected away, so every other outbox event
	// must have produced exactly one notification.
	if uint64(len(seen))+50000 != ExpectedCounts().Outbox {
		t.Fatalf("projected notifications=%d do not cover the outbox facts", len(seen))
	}
}
