package events_test

import (
	"encoding/json"
	"testing"

	"github.com/necrogami/kanboard/internal/core/events"
)

func TestNewEncodesPayload(t *testing.T) {
	ev, err := events.New(events.CardMoved, events.CardMovedPayload{
		FromColumnID: "c1", ToColumnID: "c2", FromName: "Todo", ToName: "Done",
		FromCategory: "todo", ToCategory: "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != events.CardMoved {
		t.Fatalf("kind = %q", ev.Kind)
	}
	var p events.CardMovedPayload
	if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
		t.Fatal(err)
	}
	if p.ToCategory != "done" || p.FromName != "Todo" {
		t.Fatalf("payload round trip = %+v", p)
	}
}

func TestNewRejectsUnknownKind(t *testing.T) {
	if _, err := events.New(events.Kind("nope"), nil); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

// specKinds is the spec 4.3 list for 0.1 minus the kinds owned by later
// plans (user.deleted, import.*, webhook.*). Adding a kind means adding
// it here too; the contract is frozen, so this list only grows.
var specKinds = []string{
	"workspace.created", "project.created", "project.updated", "board.created", "board.updated",
	"column.created", "column.updated", "column.archived", "column.restored",
	"card.created", "card.updated", "card.moved", "card.archived", "card.restored",
	"comment.added", "comment.edited", "comment.deleted",
	"label.created", "label.added", "label.removed", "assignee.added", "assignee.removed",
	"member.added", "member.removed", "member.role_changed",
}

func TestKindsMatchSpec(t *testing.T) {
	got := map[string]bool{}
	for _, k := range events.Kinds() {
		got[string(k)] = true
	}
	for _, want := range specKinds {
		if !got[want] {
			t.Errorf("kind %q from spec 4.3 is not defined", want)
		}
		if !events.Known(events.Kind(want)) {
			t.Errorf("Known(%q) = false", want)
		}
	}
	if len(got) != len(specKinds) {
		t.Errorf("%d kinds defined, %d in the spec list; update specKinds when adding a kind", len(got), len(specKinds))
	}
}
