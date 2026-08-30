package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/keys"
	"github.com/necrogami/kanboard/internal/core/order"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// loadCard resolves PROJ-42 within the actor's workspace.
func (t *Tx) loadCard(key string) (sqlitegen.Card, sqlitegen.Project, error) {
	projectKey, number, err := keys.ParseCard(key)
	if err != nil {
		return sqlitegen.Card{}, sqlitegen.Project{}, validation(err)
	}
	p, err := t.Q.GetProjectByKey(t.ctx, sqlitegen.GetProjectByKeyParams{WorkspaceID: t.actor.WorkspaceID, Key: projectKey})
	if isNoRows(err) {
		return sqlitegen.Card{}, sqlitegen.Project{}, notFound("card")
	}
	if err != nil {
		return sqlitegen.Card{}, sqlitegen.Project{}, err
	}
	c, err := t.Q.GetCardByNumber(t.ctx, sqlitegen.GetCardByNumberParams{ProjectID: p.ID, Number: number})
	if isNoRows(err) {
		return sqlitegen.Card{}, sqlitegen.Project{}, notFound("card")
	}
	if err != nil {
		return sqlitegen.Card{}, sqlitegen.Project{}, err
	}
	return c, p, nil
}

func (t *Tx) checkVersion(current int64) error {
	if t.meta.ExpectedVersion != 0 && t.meta.ExpectedVersion != current {
		return conflict("card was modified", current)
	}
	return nil
}

// cardResult builds the DTO including labels and assignees.
func (t *Tx) cardResult(p sqlitegen.Project, c sqlitegen.Card) (Card, error) {
	labels, err := t.Q.ListCardLabelIDs(t.ctx, c.ID)
	if err != nil {
		return Card{}, err
	}
	assignees, err := t.Q.ListCardAssigneeIDs(t.ctx, c.ID)
	if err != nil {
		return Card{}, err
	}
	return cardDTO(p.Key, c, labels, assignees), nil
}

// positionOrEmpty turns sql.ErrNoRows into the open bound.
func positionOrEmpty(pos string, err error) (string, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pos, err
}

// CreateCard adds a card at the bottom of a column.
func (s *Service) CreateCard(ctx context.Context, actor policy.Actor, cmd commands.CreateCard) (Card, error) {
	if err := cmd.Validate(); err != nil {
		return Card{}, validation(err)
	}
	var out Card
	err := s.run(ctx, actor, cmd.Meta, "CreateCard", &out, func(tx *Tx) error {
		p, err := tx.Q.GetProject(tx.ctx, cmd.ProjectID)
		if isNoRows(err) || (err == nil && p.WorkspaceID != actor.WorkspaceID) {
			return notFound("project")
		}
		if err != nil {
			return err
		}
		b, err := tx.Q.GetBoardByProject(tx.ctx, p.ID)
		if err != nil {
			return err
		}
		if err := tx.can(policy.CardCreate, policy.Resource{ProjectID: p.ID, BoardID: b.ID}); err != nil {
			return err
		}
		var col sqlitegen.BoardColumn
		if cmd.ColumnID == "" {
			cols, err := tx.Q.ListColumns(tx.ctx, b.ID)
			if err != nil {
				return err
			}
			if len(cols) == 0 {
				return validation(errors.New("board has no columns"))
			}
			col = cols[0]
		} else {
			col, err = tx.Q.GetColumn(tx.ctx, cmd.ColumnID)
			if isNoRows(err) || (err == nil && (col.BoardID != b.ID || col.ArchivedAt.Valid)) {
				return validation(errors.New("column is not on this board"))
			}
			if err != nil {
				return err
			}
		}
		n, err := tx.Q.NextCardNumber(tx.ctx, sqlitegen.NextCardNumberParams{ID: p.ID, UpdatedAt: tx.NowMs})
		if err != nil {
			return err
		}
		last, err := positionOrEmpty(tx.Q.LastPositionInColumn(tx.ctx, col.ID))
		if err != nil {
			return err
		}
		pos, err := order.Between(last, "")
		if err != nil {
			return err
		}
		if err := tx.maybeRebalance(p.WorkspaceID, col.ID, pos); err != nil {
			return err
		}
		c, err := tx.Q.CreateCard(tx.ctx, sqlitegen.CreateCardParams{
			ID: s.newID(), WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: b.ID, ColumnID: col.ID,
			Number: n, Title: cmd.Title, Description: cmd.Description, Position: pos, DueDate: nullMs(cmd.DueDate),
			CreatedBy: actor.UserID, CreatedAt: tx.NowMs, UpdatedAt: tx.NowMs,
		})
		if err != nil {
			return err
		}
		if err := tx.emit(events.CardCreated, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: b.ID, CardID: c.ID},
			events.CardCreatedPayload{Number: n, Title: c.Title, ColumnID: col.ID, ColumnName: col.Name}); err != nil {
			return err
		}
		out, err = tx.cardResult(p, c)
		return err
	})
	return out, err
}

// errSameSpot signals that the card is already where the caller asked.
var errSameSpot = errors.New("same spot")

// isNotFound reports whether err is a service E_NOT_FOUND.
func isNotFound(err error) bool {
	var se *Error
	return errors.As(err, &se) && se.Code == CodeNotFound
}

// versioned turns the zero-rows result of a "WHERE version = ?" update
// into E_CONFLICT carrying the version now stored.
func (t *Tx) versioned(id string, row sqlitegen.Card, err error) (sqlitegen.Card, error) {
	if !isNoRows(err) {
		return row, err
	}
	cur, gerr := t.Q.GetCard(t.ctx, id)
	if gerr != nil {
		return sqlitegen.Card{}, gerr
	}
	return sqlitegen.Card{}, conflict("card was modified", cur.Version)
}

const kindRankRebalance = "rank.rebalance"

// maybeRebalance enqueues rank.rebalance for a column whose keys have
// grown past order.MaxKeyLen (spec 4.4). The row commits with the
// mutation; plan 7 registers the handler.
func (t *Tx) maybeRebalance(workspaceID, columnID, pos string) error {
	if len(pos) <= order.MaxKeyLen {
		return nil
	}
	return t.Q.InsertJob(t.ctx, sqlitegen.InsertJobParams{
		ID: t.s.newID(), WorkspaceID: nullStr(workspaceID), Kind: kindRankRebalance,
		Payload: `{"column_id":"` + columnID + `"}`, MaxAttempts: 8, RunAt: t.NowMs, CreatedAt: t.NowMs,
	})
}

// neighbourSkippingSelf finds the neighbour position on one side of
// anchor within column, skipping selfPos (the moving card's own position
// when it already lives in that column; empty otherwise). after=true
// looks above anchor; false looks below.
func neighbourSkippingSelf(tx *Tx, column, anchor, selfPos string, after bool) (string, error) {
	next := func(from string) (string, error) {
		if after {
			return positionOrEmpty(tx.Q.NextPositionAfter(tx.ctx, sqlitegen.NextPositionAfterParams{ColumnID: column, Position: from}))
		}
		return positionOrEmpty(tx.Q.PrevPositionBefore(tx.ctx, sqlitegen.PrevPositionBeforeParams{ColumnID: column, Position: from}))
	}
	pos, err := next(anchor)
	if err != nil || selfPos == "" || pos != selfPos {
		return pos, err
	}
	return next(selfPos)
}

// moveBounds returns the positions the moved card must land between.
// soft is true when a named sibling was missing, archived or in another
// column and the card goes to the bottom instead (spec 4.5). Archived
// cards keep their positions and may serve as neighbours; that only
// affects where an invisible card sits.
func moveBounds(tx *Tx, to sqlitegen.BoardColumn, moving sqlitegen.Card, cmd commands.MoveCard) (lower, upper string, soft bool, err error) {
	// Positions are unique per column only, so the moving card's own
	// position is skipped only when it is already in the target column.
	selfPos := ""
	if moving.ColumnID == to.ID {
		selfPos = moving.Position
	}
	key, after := cmd.AfterKey, true
	if cmd.BeforeKey != "" {
		key, after = cmd.BeforeKey, false
	}
	if key != "" {
		anchor, _, err := tx.loadCard(key)
		switch {
		case err != nil && !isNotFound(err):
			return "", "", false, err
		case err == nil && anchor.ID == moving.ID:
			return "", "", false, errSameSpot
		case err == nil && anchor.ColumnID == to.ID && !anchor.ArchivedAt.Valid:
			if after {
				upper, err := neighbourSkippingSelf(tx, to.ID, anchor.Position, selfPos, true)
				return anchor.Position, upper, false, err
			}
			lower, err := neighbourSkippingSelf(tx, to.ID, anchor.Position, selfPos, false)
			return lower, anchor.Position, false, err
		default:
			soft = true
		}
	}
	if cmd.Position == commands.PositionTop && !soft {
		upper, err = positionOrEmpty(tx.Q.FirstPositionInColumn(tx.ctx, to.ID))
		if err == nil && selfPos != "" && upper == selfPos {
			upper, err = positionOrEmpty(tx.Q.NextPositionAfter(tx.ctx, sqlitegen.NextPositionAfterParams{ColumnID: to.ID, Position: selfPos}))
		}
		return "", upper, false, err
	}
	lower, err = positionOrEmpty(tx.Q.LastPositionInColumn(tx.ctx, to.ID))
	if err == nil && selfPos != "" && lower == selfPos {
		lower, err = positionOrEmpty(tx.Q.PrevPositionBefore(tx.ctx, sqlitegen.PrevPositionBeforeParams{ColumnID: to.ID, Position: selfPos}))
	}
	return lower, "", soft, err
}

// MoveCard places a card in a column at the requested spot. The server
// computes the fractional key; callers never send one. The returned
// bool is the soft-conflict flag described on moveBounds.
func (s *Service) MoveCard(ctx context.Context, actor policy.Actor, cmd commands.MoveCard) (Card, bool, error) {
	if err := cmd.Validate(); err != nil {
		return Card{}, false, validation(err)
	}
	var out Card
	var soft bool
	err := s.run(ctx, actor, cmd.Meta, "MoveCard", &out, func(tx *Tx) error {
		c, p, err := tx.loadCard(cmd.CardKey)
		if err != nil {
			return err
		}
		if err := tx.can(policy.CardMove, policy.Resource{ProjectID: p.ID, BoardID: c.BoardID}); err != nil {
			return err
		}
		if err := tx.checkVersion(c.Version); err != nil {
			return err
		}
		from, err := tx.Q.GetColumn(tx.ctx, c.ColumnID)
		if err != nil {
			return err
		}
		to, err := tx.Q.GetColumn(tx.ctx, cmd.ColumnID)
		if isNoRows(err) || (err == nil && (to.BoardID != c.BoardID || to.ArchivedAt.Valid)) {
			return validation(errors.New("column is not on this board"))
		}
		if err != nil {
			return err
		}
		lower, upper, softHere, err := moveBounds(tx, to, c, cmd)
		if errors.Is(err, errSameSpot) {
			out, err = tx.cardResult(p, c)
			return err
		}
		if err != nil {
			return err
		}
		soft = softHere
		pos, err := order.Between(lower, upper)
		if err != nil {
			return err
		}
		completed := c.CompletedAt
		switch {
		case to.Category == "done" && from.Category != "done":
			completed = sql.NullInt64{Int64: tx.NowMs, Valid: true}
		case to.Category != "done" && from.Category == "done":
			completed = sql.NullInt64{}
		}
		movedRow, err := tx.Q.MoveCard(tx.ctx, sqlitegen.MoveCardParams{
			ID: c.ID, Version: c.Version, ColumnID: to.ID, Position: pos, CompletedAt: completed, UpdatedAt: tx.NowMs,
		})
		moved, err := tx.versioned(c.ID, movedRow, err)
		if err != nil {
			return err
		}
		if err := tx.maybeRebalance(p.WorkspaceID, to.ID, pos); err != nil {
			return err
		}
		if err := tx.emit(events.CardMoved, scope{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, BoardID: c.BoardID, CardID: c.ID},
			events.CardMovedPayload{FromColumnID: from.ID, ToColumnID: to.ID, FromName: from.Name, ToName: to.Name, FromCategory: from.Category, ToCategory: to.Category}); err != nil {
			return err
		}
		out, err = tx.cardResult(p, moved)
		return err
	})
	return out, soft, err
}
