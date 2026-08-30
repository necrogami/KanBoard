package service

import (
	"context"
	"errors"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// CreateLabel adds a label to a project.
func (s *Service) CreateLabel(ctx context.Context, actor policy.Actor, cmd commands.CreateLabel) (Label, error) {
	if err := cmd.Validate(); err != nil {
		return Label{}, validation(err)
	}
	var out Label
	err := s.run(ctx, actor, cmd.Meta, "CreateLabel", &out, func(tx *Tx) error {
		p, err := tx.Q.GetProject(tx.ctx, cmd.ProjectID)
		if isNoRows(err) || (err == nil && p.WorkspaceID != actor.WorkspaceID) {
			return notFound("project")
		}
		if err != nil {
			return err
		}
		if err := tx.can(policy.LabelCreate, policy.Resource{ProjectID: p.ID}); err != nil {
			return err
		}
		row, err := tx.Q.CreateLabel(tx.ctx, sqlitegen.CreateLabelParams{ID: s.newID(), WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Name: cmd.Name, Color: cmd.Color, CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		if err := tx.emit(events.LabelCreated, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID}, events.LabelPayload{LabelID: row.ID, Name: row.Name}); err != nil {
			return err
		}
		out = labelDTO(row)
		return nil
	})
	return out, err
}

// dedupe drops repeated ids from ids, keeping first-seen order, so a
// caller sending the same id twice does not produce two "add" entries.
func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func diff(current, desired []string) (add, remove []string) {
	desired = dedupe(desired)
	have := map[string]bool{}
	for _, c := range current {
		have[c] = true
	}
	want := map[string]bool{}
	for _, d := range desired {
		want[d] = true
		if !have[d] {
			add = append(add, d)
		}
	}
	for _, c := range current {
		if !want[c] {
			remove = append(remove, c)
		}
	}
	return add, remove
}

// SetCardLabels replaces a card's labels. Every label must belong to
// the card's project.
func (s *Service) SetCardLabels(ctx context.Context, actor policy.Actor, cmd commands.SetCardLabels) (Card, error) {
	if err := cmd.Validate(); err != nil {
		return Card{}, validation(err)
	}
	var out Card
	err := s.run(ctx, actor, cmd.Meta, "SetCardLabels", &out, func(tx *Tx) error {
		c, p, err := tx.loadCard(cmd.CardKey)
		if err != nil {
			return err
		}
		if err := tx.can(policy.CardUpdate, policy.Resource{ProjectID: p.ID, BoardID: c.BoardID}); err != nil {
			return err
		}
		if err := tx.checkVersion(c.Version); err != nil {
			return err
		}
		names := map[string]string{}
		for _, id := range cmd.LabelIDs {
			l, err := tx.Q.GetLabel(tx.ctx, id)
			if isNoRows(err) || (err == nil && l.ProjectID != p.ID) {
				return validation(errors.New("label " + id + " is not in this project"))
			}
			if err != nil {
				return err
			}
			names[id] = l.Name
		}
		current, err := tx.Q.ListCardLabelIDs(tx.ctx, c.ID)
		if err != nil {
			return err
		}
		add, remove := diff(current, cmd.LabelIDs)
		sc := scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: c.BoardID, CardID: c.ID}
		for _, id := range add {
			if err := tx.Q.AddCardLabel(tx.ctx, sqlitegen.AddCardLabelParams{WorkspaceID: p.WorkspaceID, CardID: c.ID, LabelID: id}); err != nil {
				return err
			}
			if err := tx.emit(events.LabelAdded, sc, events.LabelPayload{LabelID: id, Name: names[id]}); err != nil {
				return err
			}
		}
		for _, id := range remove {
			if err := tx.Q.RemoveCardLabel(tx.ctx, sqlitegen.RemoveCardLabelParams{CardID: c.ID, LabelID: id}); err != nil {
				return err
			}
			if err := tx.emit(events.LabelRemoved, sc, events.LabelPayload{LabelID: id}); err != nil {
				return err
			}
		}
		if len(add)+len(remove) > 0 {
			// Label edits are versioned card edits (the MCP card_update tool
			// carries labels), so the card version moves with them.
			touchedRow, err := tx.Q.TouchCard(tx.ctx, sqlitegen.TouchCardParams{ID: c.ID, Version: c.Version, UpdatedAt: tx.NowMs})
			c, err = tx.versioned(c.ID, touchedRow, err)
			if err != nil {
				return err
			}
		}
		out, err = tx.cardResult(p, c)
		return err
	})
	return out, err
}

// SetAssignees replaces a card's assignees. Every user must be a member
// of the project.
func (s *Service) SetAssignees(ctx context.Context, actor policy.Actor, cmd commands.SetAssignees) (Card, error) {
	if err := cmd.Validate(); err != nil {
		return Card{}, validation(err)
	}
	var out Card
	err := s.run(ctx, actor, cmd.Meta, "SetAssignees", &out, func(tx *Tx) error {
		c, p, err := tx.loadCard(cmd.CardKey)
		if err != nil {
			return err
		}
		if err := tx.can(policy.CardUpdate, policy.Resource{ProjectID: p.ID, BoardID: c.BoardID}); err != nil {
			return err
		}
		if err := tx.checkVersion(c.Version); err != nil {
			return err
		}
		members, err := tx.Q.ListProjectMembers(tx.ctx, p.ID)
		if err != nil {
			return err
		}
		isMember := map[string]bool{}
		for _, m := range members {
			isMember[m.UserID] = true
		}
		for _, uid := range cmd.UserIDs {
			if !isMember[uid] {
				return validation(errors.New("user " + uid + " is not a project member"))
			}
		}
		current, err := tx.Q.ListCardAssigneeIDs(tx.ctx, c.ID)
		if err != nil {
			return err
		}
		add, remove := diff(current, cmd.UserIDs)
		sc := scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: c.BoardID, CardID: c.ID}
		for _, uid := range add {
			if err := tx.Q.AddCardAssignee(tx.ctx, sqlitegen.AddCardAssigneeParams{WorkspaceID: p.WorkspaceID, CardID: c.ID, UserID: uid}); err != nil {
				return err
			}
			if err := tx.emit(events.AssigneeAdded, sc, events.AssigneePayload{UserID: uid}); err != nil {
				return err
			}
		}
		for _, uid := range remove {
			if err := tx.Q.RemoveCardAssignee(tx.ctx, sqlitegen.RemoveCardAssigneeParams{CardID: c.ID, UserID: uid}); err != nil {
				return err
			}
			if err := tx.emit(events.AssigneeRemoved, sc, events.AssigneePayload{UserID: uid}); err != nil {
				return err
			}
		}
		if len(add)+len(remove) > 0 {
			touchedRow, err := tx.Q.TouchCard(tx.ctx, sqlitegen.TouchCardParams{ID: c.ID, Version: c.Version, UpdatedAt: tx.NowMs})
			c, err = tx.versioned(c.ID, touchedRow, err)
			if err != nil {
				return err
			}
		}
		out, err = tx.cardResult(p, c)
		return err
	})
	return out, err
}
