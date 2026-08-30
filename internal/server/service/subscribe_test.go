package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/id"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// addWorkspaceUser creates a user in the harness workspace as a plain
// workspace member, optionally giving it a role on projectID.
func addWorkspaceUser(t *testing.T, h *harness, st *store.Store, name, projectID string, role policy.ProjectRole) policy.Actor {
	t.Helper()
	ctx := context.Background()
	q := st.Q()
	u, err := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: h.ws.ID, Name: name, Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertWorkspaceMember(ctx, sqlitegen.UpsertWorkspaceMemberParams{WorkspaceID: h.ws.ID, UserID: u.ID, Role: string(policy.WorkspaceMember), CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if projectID != "" {
		if err := q.UpsertProjectMember(ctx, sqlitegen.UpsertProjectMemberParams{WorkspaceID: h.ws.ID, ProjectID: projectID, UserID: u.ID, Role: string(role), CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := h.svc.LoadActor(ctx, h.ws.ID, u.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestSubscribeFiltersEventsTheActorCannotRead: the bus is per workspace
// because seq is per workspace, but workspace membership does not imply
// project membership and payloads carry card titles. Service.Subscribe
// is the entry point that applies the actor's read policy.
func TestSubscribeFiltersEventsTheActorCannotRead(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, _, _ := projectWithCards(t, h, "FEED", 0)

		member := addWorkspaceUser(t, h, st, "Member", p.ID, policy.ProjectViewer)
		outsider := addWorkspaceUser(t, h, st, "Outsider", "", "")

		memberSub := h.svc.Subscribe(member)
		defer memberSub.Close()
		outsiderSub := h.svc.Subscribe(outsider)
		defer outsiderSub.Close()

		if _, err := h.svc.CreateCard(ctx, h.admin, commands.CreateCard{ProjectID: p.ID, Title: "confidential"}); err != nil {
			t.Fatal(err)
		}

		ev := recvEvent(t, memberSub.C)
		if ev.Kind != events.CardCreated {
			t.Fatalf("member received %s, want %s", ev.Kind, events.CardCreated)
		}
		select {
		case ev := <-outsiderSub.C:
			t.Fatalf("non-member received %s with payload %s", ev.Kind, ev.Payload)
		case <-time.After(200 * time.Millisecond):
		}
		if outsiderSub.Evicted() {
			t.Fatal("filtered subscription was evicted")
		}
	})
}
