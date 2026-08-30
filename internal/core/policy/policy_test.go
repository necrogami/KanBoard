package policy_test

import (
	"testing"

	"github.com/necrogami/kanboard/internal/core/policy"
)

func actor(ws policy.WorkspaceRole, pr policy.ProjectRole, tok *policy.Token) policy.Actor {
	a := policy.Actor{UserID: "u1", Kind: policy.KindHuman, WorkspaceRole: ws, ProjectRoles: map[string]policy.ProjectRole{}, Token: tok}
	if pr != "" {
		a.ProjectRoles["p1"] = pr
	}
	if tok != nil {
		a.Kind = policy.KindAgent
	}
	return a
}

func TestCanMatrix(t *testing.T) {
	res := policy.Resource{ProjectID: "p1", BoardID: "b1"}
	other := policy.Resource{ProjectID: "p2", BoardID: "b2"}
	readTok := &policy.Token{ID: "t", Scopes: []policy.Scope{policy.ScopeRead}}
	writeTok := &policy.Token{ID: "t", Scopes: []policy.Scope{policy.ScopeWrite}}
	boardTok := &policy.Token{ID: "t", Scopes: []policy.Scope{policy.ScopeWrite}, BoardIDs: []string{"b9"}}
	adminTok := &policy.Token{ID: "t", Scopes: []policy.Scope{policy.ScopeAdmin}}

	cases := []struct {
		name string
		a    policy.Actor
		act  policy.Action
		res  policy.Resource
		want bool
	}{
		{"viewer reads", actor(policy.WorkspaceMember, policy.ProjectViewer, nil), policy.ProjectRead, res, true},
		{"viewer cannot create", actor(policy.WorkspaceMember, policy.ProjectViewer, nil), policy.CardCreate, res, false},
		{"member creates", actor(policy.WorkspaceMember, policy.ProjectMember, nil), policy.CardCreate, res, true},
		{"member cannot admin project", actor(policy.WorkspaceMember, policy.ProjectMember, nil), policy.ProjectAdmin, res, false},
		{"project admin admins project", actor(policy.WorkspaceMember, policy.ProjectAdminRole, nil), policy.ProjectAdmin, res, true},
		{"project admin is not workspace admin", actor(policy.WorkspaceMember, policy.ProjectAdminRole, nil), policy.WorkspaceAdmin, res, false},
		{"workspace admin does anything", actor(policy.WorkspaceAdminRole, "", nil), policy.CardArchive, other, true},
		{"non member sees nothing", actor(policy.WorkspaceMember, "", nil), policy.ProjectRead, res, false},
		{"member of p1 not p2", actor(policy.WorkspaceMember, policy.ProjectMember, nil), policy.ProjectRead, other, false},
		{"read token cannot write", actor(policy.WorkspaceMember, policy.ProjectMember, readTok), policy.CardCreate, res, false},
		{"read token reads", actor(policy.WorkspaceMember, policy.ProjectMember, readTok), policy.ProjectRead, res, true},
		{"write token implies read", actor(policy.WorkspaceMember, policy.ProjectMember, writeTok), policy.ProjectRead, res, true},
		{"write token cannot admin", actor(policy.WorkspaceMember, policy.ProjectAdminRole, writeTok), policy.ProjectAdmin, res, false},
		{"board restricted token blocked", actor(policy.WorkspaceMember, policy.ProjectMember, boardTok), policy.CardCreate, res, false},
		{"board restricted token allowed", actor(policy.WorkspaceMember, policy.ProjectMember, boardTok), policy.CardCreate, policy.Resource{ProjectID: "p1", BoardID: "b9"}, true},
		{"token does not lift role", actor(policy.WorkspaceMember, policy.ProjectViewer, writeTok), policy.CardCreate, res, false},
		{"admin token allows project admin", actor(policy.WorkspaceMember, policy.ProjectAdminRole, adminTok), policy.ProjectAdmin, res, true},
		{"admin token implies write", actor(policy.WorkspaceMember, policy.ProjectAdminRole, adminTok), policy.CardCreate, res, true},
		{"admin token implies read", actor(policy.WorkspaceMember, policy.ProjectAdminRole, adminTok), policy.ProjectRead, res, true},
		{"admin token does not lift role", actor(policy.WorkspaceMember, policy.ProjectMember, adminTok), policy.ProjectAdmin, res, false},
		{"board restricted token reads project without board", actor(policy.WorkspaceMember, policy.ProjectMember, boardTok), policy.ProjectRead, policy.Resource{ProjectID: "p1"}, true},
		{"board restricted token blocks write without board", actor(policy.WorkspaceMember, policy.ProjectMember, boardTok), policy.CardCreate, policy.Resource{ProjectID: "p1"}, false},
		{"unknown action denied", actor(policy.WorkspaceMember, policy.ProjectAdminRole, nil), policy.Action("nope"), res, false},
		{"member creates project", actor(policy.WorkspaceMember, "", nil), policy.ProjectCreate, policy.Resource{}, true},
		{"outsider cannot create project", actor("", "", nil), policy.ProjectCreate, policy.Resource{}, false},
		{"read token cannot create project", actor(policy.WorkspaceMember, "", readTok), policy.ProjectCreate, policy.Resource{}, false},
	}
	for _, c := range cases {
		if got := policy.Can(c.a, c.act, c.res); got != c.want {
			t.Errorf("%s: Can = %v, want %v", c.name, got, c.want)
		}
	}
}
