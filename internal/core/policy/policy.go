// Package policy answers "may this actor perform this action on this
// resource" as a pure function. Roles come from membership rows; token
// scopes and board restrictions can only narrow what the role allows.
package policy

// WorkspaceRole is a user's role in the workspace.
type WorkspaceRole string

// ProjectRole is a user's role in one project.
type ProjectRole string

// Scope is an API token scope.
type Scope string

// Action is something an actor wants to do.
type Action string

// Kind distinguishes people from agents acting through tokens.
type Kind string

const (
	WorkspaceAdminRole WorkspaceRole = "admin"
	WorkspaceMember    WorkspaceRole = "member"

	ProjectAdminRole ProjectRole = "admin"
	ProjectMember    ProjectRole = "member"
	ProjectViewer    ProjectRole = "viewer"

	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
	ScopeAdmin Scope = "admin"

	KindHuman Kind = "human"
	KindAgent Kind = "agent"

	ProjectRead    Action = "project.read"
	ProjectAdmin   Action = "project.admin"
	WorkspaceAdmin Action = "workspace.admin"
	CardCreate     Action = "card.create"
	CardUpdate     Action = "card.update"
	CardMove       Action = "card.move"
	CardArchive    Action = "card.archive"
	CardRestore    Action = "card.restore"
	CommentAdd     Action = "comment.add"
	LabelCreate    Action = "label.create"
	ProjectCreate  Action = "project.create"
)

// Token describes the API token an agent is acting through. A nil Token
// means a browser or app session.
type Token struct {
	ID       string
	Scopes   []Scope
	BoardIDs []string // nil or empty means every board
}

// Actor is who is asking.
type Actor struct {
	UserID        string
	WorkspaceID   string
	Kind          Kind
	WorkspaceRole WorkspaceRole
	ProjectRoles  map[string]ProjectRole
	Token         *Token
}

// Resource is what the action targets.
type Resource struct {
	ProjectID string
	BoardID   string
}

func needsScope(act Action) Scope {
	switch act {
	case ProjectRead:
		return ScopeRead
	case ProjectAdmin, WorkspaceAdmin:
		return ScopeAdmin
	default:
		return ScopeWrite
	}
}

func scopeRank(s Scope) int {
	switch s {
	case ScopeRead:
		return 1
	case ScopeWrite:
		return 2
	case ScopeAdmin:
		return 3
	}
	return 0
}

func tokenAllows(t *Token, act Action, r Resource) bool {
	if t == nil {
		return true
	}
	best := 0
	for _, s := range t.Scopes {
		if scopeRank(s) > best {
			best = scopeRank(s)
		}
	}
	if best < scopeRank(needsScope(act)) {
		return false
	}
	if len(t.BoardIDs) == 0 {
		return true
	}
	if r.BoardID == "" {
		return act == ProjectRead
	}
	for _, b := range t.BoardIDs {
		if b == r.BoardID {
			return true
		}
	}
	return false
}

// knownActions lists every Action this package understands. Can denies
// anything else outright; a later task extends this set alongside its
// new Action constants.
var knownActions = map[Action]bool{
	ProjectRead:    true,
	ProjectAdmin:   true,
	WorkspaceAdmin: true,
	CardCreate:     true,
	CardUpdate:     true,
	CardMove:       true,
	CardArchive:    true,
	CardRestore:    true,
	CommentAdd:     true,
	LabelCreate:    true,
	ProjectCreate:  true,
}

// Can reports whether a may perform act on r.
func Can(a Actor, act Action, r Resource) bool {
	if !tokenAllows(a.Token, act, r) {
		return false
	}
	if !knownActions[act] {
		return false
	}
	if a.WorkspaceRole == WorkspaceAdminRole {
		return true
	}
	if act == WorkspaceAdmin {
		return false
	}
	if act == ProjectCreate {
		return a.WorkspaceRole != ""
	}
	role, ok := a.ProjectRoles[r.ProjectID]
	if !ok {
		return false
	}
	switch act {
	case ProjectRead:
		return true
	case ProjectAdmin:
		return role == ProjectAdminRole
	default:
		return role == ProjectAdminRole || role == ProjectMember
	}
}
