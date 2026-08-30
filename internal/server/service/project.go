package service

import (
	"context"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/order"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// CreateWorkspace bootstraps the install. It succeeds only once: 0.1 is
// single-workspace, so a second call returns E_CONFLICT.
func (s *Service) CreateWorkspace(ctx context.Context, cmd commands.CreateWorkspace) (Workspace, error) {
	if err := cmd.Validate(); err != nil {
		return Workspace{}, validation(err)
	}
	var out Workspace
	system := policy.Actor{Kind: policy.KindHuman}
	err := s.run(ctx, system, cmd.Meta, "CreateWorkspace", &out, func(tx *Tx) error {
		n, err := tx.Q.CountWorkspaces(tx.ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return conflict("workspace already exists", 0)
		}
		ws, err := tx.Q.CreateWorkspace(tx.ctx, sqlitegen.CreateWorkspaceParams{ID: s.newID(), Name: cmd.Name, Slug: cmd.Slug, CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		u, err := tx.Q.CreateUser(tx.ctx, sqlitegen.CreateUserParams{ID: s.newID(), WorkspaceID: ws.ID, Email: nullStr(cmd.AdminEmail), Name: cmd.AdminName, Kind: string(policy.KindHuman), Locale: "en", CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		if err := tx.Q.UpsertWorkspaceMember(tx.ctx, sqlitegen.UpsertWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: u.ID, Role: string(policy.WorkspaceAdminRole), CreatedAt: tx.NowMs}); err != nil {
			return err
		}
		if err := tx.emit(events.WorkspaceCreated, scope{WorkspaceID: ws.ID}, nil); err != nil {
			return err
		}
		if err := tx.emit(events.MemberAdded, scope{WorkspaceID: ws.ID}, events.MemberPayload{UserID: u.ID, Role: string(policy.WorkspaceAdminRole)}); err != nil {
			return err
		}
		out = Workspace{ID: ws.ID, Name: ws.Name, Slug: ws.Slug, AdminUserID: u.ID}
		return nil
	})
	return out, err
}

var defaultColumns = []struct{ name, category string }{
	{"To do", "todo"}, {"In progress", "in_progress"}, {"Done", "done"},
}

// CreateProject creates a project, its board and the three default
// columns, and makes the actor its admin.
func (s *Service) CreateProject(ctx context.Context, actor policy.Actor, cmd commands.CreateProject) (Project, error) {
	if err := cmd.Validate(); err != nil {
		return Project{}, validation(err)
	}
	var out Project
	err := s.run(ctx, actor, cmd.Meta, "CreateProject", &out, func(tx *Tx) error {
		if cmd.WorkspaceID != actor.WorkspaceID {
			return forbidden()
		}
		if err := tx.can(policy.ProjectCreate, policy.Resource{}); err != nil {
			return err
		}
		if _, err := tx.Q.GetProjectByKey(tx.ctx, sqlitegen.GetProjectByKeyParams{WorkspaceID: cmd.WorkspaceID, Key: cmd.Key}); err == nil {
			return conflict("project key already exists", 0)
		} else if !isNoRows(err) {
			return err
		}
		p, err := tx.Q.CreateProject(tx.ctx, sqlitegen.CreateProjectParams{ID: s.newID(), WorkspaceID: cmd.WorkspaceID, Key: cmd.Key, Name: cmd.Name, CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		if err := tx.Q.UpsertProjectMember(tx.ctx, sqlitegen.UpsertProjectMemberParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, UserID: actor.UserID, Role: string(policy.ProjectAdminRole), CreatedAt: tx.NowMs}); err != nil {
			return err
		}
		b, err := tx.Q.CreateBoard(tx.ctx, sqlitegen.CreateBoardParams{ID: s.newID(), WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Name: cmd.Name, Position: "V", CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		if err := tx.emit(events.ProjectCreated, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID}, events.ProjectPayload{Key: p.Key, Name: p.Name}); err != nil {
			return err
		}
		if err := tx.emit(events.BoardCreated, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: b.ID}, events.BoardPayload{Name: b.Name}); err != nil {
			return err
		}
		positions := order.Rebalance(len(defaultColumns))
		for i, dc := range defaultColumns {
			c, err := tx.Q.CreateColumn(tx.ctx, sqlitegen.CreateColumnParams{ID: s.newID(), WorkspaceID: p.WorkspaceID, BoardID: b.ID, Name: dc.name, Position: positions[i], Category: dc.category, CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
			if err != nil {
				return err
			}
			if err := tx.emit(events.ColumnCreated, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: b.ID}, events.ColumnPayload{ColumnID: c.ID, Name: c.Name, Category: c.Category}); err != nil {
				return err
			}
		}
		out = Project{ID: p.ID, WorkspaceID: p.WorkspaceID, Key: p.Key, Name: p.Name, Version: p.Version, BoardID: b.ID}
		return nil
	})
	return out, err
}

// GetBoard returns the project's board, its columns and its active
// cards for readers of the project.
func (s *Service) GetBoard(ctx context.Context, actor policy.Actor, projectKey string) (Board, []Card, error) {
	q := s.st.Q()
	p, err := q.GetProjectByKey(ctx, sqlitegen.GetProjectByKeyParams{WorkspaceID: actor.WorkspaceID, Key: projectKey})
	if isNoRows(err) {
		return Board{}, nil, notFound("project")
	}
	if err != nil {
		return Board{}, nil, err
	}
	b, err := q.GetBoardByProject(ctx, p.ID)
	if err != nil {
		return Board{}, nil, err
	}
	if !policy.Can(actor, policy.ProjectRead, policy.Resource{ProjectID: p.ID, BoardID: b.ID}) {
		return Board{}, nil, forbidden()
	}
	cols, err := q.ListColumns(ctx, b.ID)
	if err != nil {
		return Board{}, nil, err
	}
	out := Board{ID: b.ID, ProjectID: b.ProjectID, Name: b.Name, Version: b.Version, Columns: []Column{}}
	for _, c := range cols {
		out.Columns = append(out.Columns, columnDTO(c))
	}
	rows, err := q.ListCardsByBoard(ctx, b.ID)
	if err != nil {
		return Board{}, nil, err
	}
	// Two board-scoped queries rather than a pair per card: a 2000 card
	// board is the spec 12.5 baseline and was 4001 queries.
	labelRows, err := q.ListCardLabelIDsByBoard(ctx, b.ID)
	if err != nil {
		return Board{}, nil, err
	}
	labels := map[string][]string{}
	for _, r := range labelRows {
		labels[r.CardID] = append(labels[r.CardID], r.LabelID)
	}
	assigneeRows, err := q.ListCardAssigneeIDsByBoard(ctx, b.ID)
	if err != nil {
		return Board{}, nil, err
	}
	assignees := map[string][]string{}
	for _, r := range assigneeRows {
		assignees[r.CardID] = append(assignees[r.CardID], r.UserID)
	}
	cards := make([]Card, 0, len(rows))
	for _, r := range rows {
		cards = append(cards, cardDTO(p.Key, r, labels[r.ID], assignees[r.ID]))
	}
	return out, cards, nil
}
