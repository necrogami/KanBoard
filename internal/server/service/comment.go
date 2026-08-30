package service

import (
	"context"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// AddComment appends a comment to a card.
func (s *Service) AddComment(ctx context.Context, actor policy.Actor, cmd commands.AddComment) (Comment, error) {
	if err := cmd.Validate(); err != nil {
		return Comment{}, validation(err)
	}
	var out Comment
	err := s.run(ctx, actor, cmd.Meta, "AddComment", &out, func(tx *Tx) error {
		c, p, err := tx.loadCard(cmd.CardKey)
		if err != nil {
			return err
		}
		if err := tx.can(policy.CommentAdd, policy.Resource{ProjectID: p.ID, BoardID: c.BoardID}); err != nil {
			return err
		}
		row, err := tx.Q.CreateComment(tx.ctx, sqlitegen.CreateCommentParams{ID: s.newID(), WorkspaceID: p.WorkspaceID, CardID: c.ID, AuthorID: actor.UserID, Body: cmd.Body, ViaTokenID: nullStr(cmd.ViaTokenID), CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		if err := tx.emit(events.CommentAdded, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: c.BoardID, CardID: c.ID}, events.CommentPayload{CommentID: row.ID}); err != nil {
			return err
		}
		out = commentDTO(row)
		return nil
	})
	return out, err
}
