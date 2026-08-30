package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/filter"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// cardColumns must list card's columns in the exact order of the
// sqlitegen.Card struct (which follows the migration).
const cardColumns = "id, workspace_id, project_id, board_id, column_id, number, title, description, position, due_date, created_by, completed_at, archived_at, version, type_id, parent_id, priority, estimate, start_date, resolution, iteration_id, created_at, updated_at"

// SearchCards runs the structured filter against one project. The
// predicate set is dynamic, so this is the one hand-written query;
// results are ordered by id (UUIDv7, creation order) with an opaque
// cursor for the next page.
func SearchCards(ctx context.Context, db sqlitegen.DBTX, d Dialect, projectID string, f filter.Filter) ([]sqlitegen.Card, string, error) {
	var where []string
	var args []any
	ph := func(v any) string {
		args = append(args, v)
		if d == Postgres {
			return "$" + strconv.Itoa(len(args))
		}
		return "?"
	}
	where = append(where, "project_id = "+ph(projectID))
	if f.Archived != nil && *f.Archived {
		where = append(where, "archived_at IS NOT NULL")
	} else {
		where = append(where, "archived_at IS NULL")
	}
	if len(f.ColumnIDs) > 0 {
		where = append(where, "column_id IN ("+phList(ph, f.ColumnIDs)+")")
	}
	if len(f.LabelIDs) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM card_label cl WHERE cl.card_id = card.id AND cl.label_id IN ("+phList(ph, f.LabelIDs)+"))")
	}
	if len(f.AssigneeIDs) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM card_assignee ca WHERE ca.card_id = card.id AND ca.user_id IN ("+phList(ph, f.AssigneeIDs)+"))")
	}
	if f.UpdatedAfter != nil {
		where = append(where, "updated_at > "+ph(clock.Millis(*f.UpdatedAfter)))
	}
	if f.Text != "" {
		// lower() on both sides: SQLite's LIKE is case-insensitive for ASCII
		// only, Postgres's is case-sensitive; this makes both behave alike.
		pat := "%" + escapeLike(strings.ToLower(f.Text)) + "%"
		where = append(where, "(lower(title) LIKE "+ph(pat)+" ESCAPE '\\' OR lower(description) LIKE "+ph(pat)+" ESCAPE '\\')")
	}
	if f.Cursor != "" {
		where = append(where, "id > "+ph(f.Cursor))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = filter.DefaultLimit
	}
	query := "SELECT " + cardColumns + " FROM card WHERE " + strings.Join(where, " AND ") + " ORDER BY id LIMIT " + ph(limit+1)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	var out []sqlitegen.Card
	for rows.Next() {
		var c sqlitegen.Card
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.ProjectID, &c.BoardID, &c.ColumnID, &c.Number, &c.Title, &c.Description, &c.Position, &c.DueDate, &c.CreatedBy, &c.CompletedAt, &c.ArchivedAt, &c.Version, &c.TypeID, &c.ParentID, &c.Priority, &c.Estimate, &c.StartDate, &c.Resolution, &c.IterationID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	if out == nil {
		out = []sqlitegen.Card{}
	}
	return out, next, nil
}

func phList(ph func(any) string, vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = ph(v)
	}
	return strings.Join(parts, ", ")
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
