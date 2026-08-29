package service

import (
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/keys"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// Workspace is the bootstrap result.
type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	AdminUserID string `json:"admin_user_id"`
}

// Project is a project with its (single, in 0.1) board id.
type Project struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Version     int64  `json:"version"`
	BoardID     string `json:"board_id"`
}

// Column is one board column.
type Column struct {
	ID       string `json:"id"`
	BoardID  string `json:"board_id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Category string `json:"category"`
	WipLimit *int64 `json:"wip_limit"`
	Version  int64  `json:"version"`
}

// Board is a board with its columns.
type Board struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	Name      string   `json:"name"`
	Version   int64    `json:"version"`
	Columns   []Column `json:"columns"`
}

// Card is the card shape every adapter exposes.
type Card struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	ProjectID   string     `json:"project_id"`
	BoardID     string     `json:"board_id"`
	ColumnID    string     `json:"column_id"`
	Number      int64      `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Position    string     `json:"position"`
	DueDate     *time.Time `json:"due_date"`
	CompletedAt *time.Time `json:"completed_at"`
	ArchivedAt  *time.Time `json:"archived_at"`
	CreatedBy   string     `json:"created_by"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LabelIDs    []string   `json:"label_ids"`
	AssigneeIDs []string   `json:"assignee_ids"`
}

// Comment is one card comment.
type Comment struct {
	ID         string    `json:"id"`
	CardID     string    `json:"card_id"`
	AuthorID   string    `json:"author_id"`
	Body       string    `json:"body"`
	ViaTokenID string    `json:"via_token_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// Label is a project label.
type Label struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
}

func columnDTO(c sqlitegen.BoardColumn) Column {
	var wip *int64
	if c.WipLimit.Valid {
		v := c.WipLimit.Int64
		wip = &v
	}
	return Column{ID: c.ID, BoardID: c.BoardID, Name: c.Name, Position: c.Position, Category: c.Category, WipLimit: wip, Version: c.Version}
}

func cardDTO(projectKey string, c sqlitegen.Card, labels, assignees []string) Card {
	if labels == nil {
		labels = []string{}
	}
	if assignees == nil {
		assignees = []string{}
	}
	return Card{
		ID: c.ID, Key: keys.Card(projectKey, c.Number), ProjectID: c.ProjectID, BoardID: c.BoardID, ColumnID: c.ColumnID,
		Number: c.Number, Title: c.Title, Description: c.Description, Position: c.Position,
		DueDate: msPtr(c.DueDate), CompletedAt: msPtr(c.CompletedAt), ArchivedAt: msPtr(c.ArchivedAt),
		CreatedBy: c.CreatedBy, Version: c.Version,
		CreatedAt: clock.FromMillis(c.CreatedAt), UpdatedAt: clock.FromMillis(c.UpdatedAt),
		LabelIDs: labels, AssigneeIDs: assignees,
	}
}

func commentDTO(c sqlitegen.Comment) Comment {
	return Comment{ID: c.ID, CardID: c.CardID, AuthorID: c.AuthorID, Body: c.Body, ViaTokenID: strPtr(c.ViaTokenID), CreatedAt: clock.FromMillis(c.CreatedAt)}
}

func labelDTO(l sqlitegen.Label) Label {
	return Label{ID: l.ID, ProjectID: l.ProjectID, Name: l.Name, Color: l.Color}
}
