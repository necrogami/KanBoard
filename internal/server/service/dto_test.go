package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// TestRankKeysAreNotEncoded: spec 4.4 says clients express intent as
// after or before a sibling, or top or bottom, and never see or send a
// rank string. Order is carried by array order instead. Plan 3 freezes
// an OpenAPI snapshot and plan 4 an MCP tool list, after which removing
// a field is a compatibility break.
func TestRankKeysAreNotEncoded(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		_, board, cards := projectWithCards(t, h, "RANK", 2)

		if cards[0].Position == "" {
			t.Fatal("the Go field is still the server's rank key and must stay populated")
		}
		for what, v := range map[string]any{"card": cards[0], "column": board.Columns[0], "board": board} {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), `"position"`) {
				t.Fatalf("%s JSON carries a rank string: %s", what, b)
			}
		}
		// The board snapshot is still ordered, which is how a client learns
		// the order.
		_, ordered, err := h.svc.GetBoard(context.Background(), h.admin, "RANK")
		if err != nil {
			t.Fatal(err)
		}
		if len(ordered) != 2 || ordered[0].Key != cards[0].Key {
			t.Fatalf("board cards out of order: %v", ordered)
		}
	})
}
