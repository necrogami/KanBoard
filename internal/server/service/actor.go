package service

import (
	"context"

	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// TokenInfo is what the auth layer knows about an API token.
type TokenInfo struct {
	ID       string
	Scopes   []policy.Scope
	BoardIDs []string
}

// LoadActor builds the policy actor for a user, optionally acting
// through a token. Disabled or deleted users are forbidden.
func (s *Service) LoadActor(ctx context.Context, workspaceID, userID string, tok *TokenInfo) (policy.Actor, error) {
	q := s.st.Q()
	u, err := q.GetUser(ctx, userID)
	if isNoRows(err) {
		return policy.Actor{}, forbidden()
	}
	if err != nil {
		return policy.Actor{}, err
	}
	if u.DisabledAt.Valid || u.DeletedAt.Valid || u.WorkspaceID != workspaceID {
		return policy.Actor{}, forbidden()
	}
	a := policy.Actor{UserID: userID, WorkspaceID: workspaceID, Kind: policy.KindHuman, ProjectRoles: map[string]policy.ProjectRole{}}
	wm, err := q.GetWorkspaceMember(ctx, sqlitegen.GetWorkspaceMemberParams{WorkspaceID: workspaceID, UserID: userID})
	if err == nil {
		a.WorkspaceRole = policy.WorkspaceRole(wm.Role)
	} else if !isNoRows(err) {
		return policy.Actor{}, err
	}
	pms, err := q.ListProjectMembershipsForUser(ctx, userID)
	if err != nil {
		return policy.Actor{}, err
	}
	for _, pm := range pms {
		a.ProjectRoles[pm.ProjectID] = policy.ProjectRole(pm.Role)
	}
	if tok != nil {
		a.Kind = policy.KindAgent
		a.Token = &policy.Token{ID: tok.ID, Scopes: tok.Scopes, BoardIDs: tok.BoardIDs}
	}
	return a, nil
}
