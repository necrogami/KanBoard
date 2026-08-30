package service_test

import (
	"context"
	"testing"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/filter"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/service"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// TestBoardRestrictedTokenAtTheServiceSeam pins the two callers that
// asked policy about a project without naming its board. A board
// restriction can only narrow, so a token scoped to one board must not
// read another board's project, and must still be allowed to write on
// the board it is scoped to.
func TestBoardRestrictedTokenAtTheServiceSeam(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		mine, mineBoard, _ := projectWithCards(t, h, "MINE", 1)
		other, _, _ := projectWithCards(t, h, "OTHER", 1)

		tok := &service.TokenInfo{ID: "t1", Scopes: []policy.Scope{policy.ScopeWrite}, BoardIDs: []string{mineBoard.ID}}
		agent, err := h.svc.LoadActor(ctx, h.ws.ID, h.ws.AdminUserID, tok)
		if err != nil {
			t.Fatal(err)
		}

		if _, _, err := h.svc.SearchCards(ctx, agent, "MINE", filter.Filter{}); err != nil {
			t.Fatal("search on the token's own board:", err)
		}
		_, _, err = h.svc.SearchCards(ctx, agent, "OTHER", filter.Filter{})
		_ = code(t, err, service.CodeForbidden)

		if _, err := h.svc.CreateLabel(ctx, agent, commands.CreateLabel{ProjectID: mine.ID, Name: "bug", Color: "#FF0000"}); err != nil {
			t.Fatal("label on the token's own board:", err)
		}
		_, err = h.svc.CreateLabel(ctx, agent, commands.CreateLabel{ProjectID: other.ID, Name: "bug", Color: "#FF0000"})
		_ = code(t, err, service.CodeForbidden)
	})
}
