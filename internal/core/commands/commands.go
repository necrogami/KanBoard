// Package commands defines every mutation KanBoard accepts, with
// validation that does not need a database. Adapters (web, REST, MCP)
// build these; the service layer executes them.
package commands

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/necrogami/kanboard/internal/core/keys"
)

const (
	MaxTitle       = 500
	MaxDescription = 100000
	MaxComment     = 20000
	MaxName        = 200

	PositionTop    = "top"
	PositionBottom = "bottom"
)

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// ValidationError names the offending field.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Msg) }

func invalid(field, msg string) error { return &ValidationError{Field: field, Msg: msg} }

func required(field, v string) error {
	if strings.TrimSpace(v) == "" {
		return invalid(field, "required")
	}
	return nil
}

func maxLen(field, v string, n int) error {
	if len(v) > n {
		return invalid(field, fmt.Sprintf("longer than %d bytes", n))
	}
	return nil
}

// Meta carries who is acting and the idempotency and concurrency guards.
type Meta struct {
	ActorUserID     string
	ViaTokenID      string
	IdempotencyKey  string
	ExpectedVersion int64
}

// CreateWorkspace bootstraps the install: one workspace and its first admin.
type CreateWorkspace struct {
	Meta
	Name       string
	Slug       string
	AdminEmail string
	AdminName  string
}

func (c CreateWorkspace) Validate() error {
	if err := required("name", c.Name); err != nil {
		return err
	}
	if !slugRe.MatchString(c.Slug) {
		return invalid("slug", "lowercase letters, digits and single dashes")
	}
	if !emailRe.MatchString(c.AdminEmail) {
		return invalid("admin_email", "not an email address")
	}
	return required("admin_name", c.AdminName)
}

// CreateProject creates a project with its board and default columns.
type CreateProject struct {
	Meta
	WorkspaceID string
	Key         string
	Name        string
}

func (c CreateProject) Validate() error {
	if err := required("workspace_id", c.WorkspaceID); err != nil {
		return err
	}
	if err := keys.ValidateProjectKey(c.Key); err != nil {
		return invalid("key", err.Error())
	}
	if err := required("name", c.Name); err != nil {
		return err
	}
	return maxLen("name", c.Name, MaxName)
}

// CreateCard adds a card to a column (the first column when ColumnID is empty).
type CreateCard struct {
	Meta
	ProjectID   string
	ColumnID    string
	Title       string
	Description string
	DueDate     *time.Time
}

func (c CreateCard) Validate() error {
	if err := required("project_id", c.ProjectID); err != nil {
		return err
	}
	if err := required("title", c.Title); err != nil {
		return err
	}
	if err := maxLen("title", c.Title, MaxTitle); err != nil {
		return err
	}
	return maxLen("description", c.Description, MaxDescription)
}

// UpdateCard patches fields; nil pointers leave a field untouched.
type UpdateCard struct {
	Meta
	CardKey      string
	Title        *string
	Description  *string
	DueDate      *time.Time
	ClearDueDate bool
}

func (c UpdateCard) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	if c.Title == nil && c.Description == nil && c.DueDate == nil && !c.ClearDueDate {
		return invalid("patch", "no fields to update")
	}
	if c.Title != nil {
		if err := required("title", *c.Title); err != nil {
			return err
		}
		if err := maxLen("title", *c.Title, MaxTitle); err != nil {
			return err
		}
	}
	if c.Description != nil {
		if err := maxLen("description", *c.Description, MaxDescription); err != nil {
			return err
		}
	}
	if c.DueDate != nil && c.ClearDueDate {
		return invalid("due_date", "cannot set and clear at once")
	}
	return nil
}

// MoveCard places a card in a column: after or before a sibling, or at
// the top or bottom. With none given, the card goes to the bottom.
type MoveCard struct {
	Meta
	CardKey   string
	ColumnID  string
	AfterKey  string
	BeforeKey string
	Position  string
}

func (c MoveCard) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	if err := required("column_id", c.ColumnID); err != nil {
		return err
	}
	set := 0
	if c.AfterKey != "" {
		set++
		if _, _, err := keys.ParseCard(c.AfterKey); err != nil {
			return invalid("after_key", err.Error())
		}
	}
	if c.BeforeKey != "" {
		set++
		if _, _, err := keys.ParseCard(c.BeforeKey); err != nil {
			return invalid("before_key", err.Error())
		}
	}
	if c.Position != "" {
		set++
		if c.Position != PositionTop && c.Position != PositionBottom {
			return invalid("position", "top or bottom")
		}
	}
	if set > 1 {
		return invalid("after_key", "give only one of after_key, before_key, position")
	}
	return nil
}

// ArchiveCard soft-deletes a card.
type ArchiveCard struct {
	Meta
	CardKey string
}

func (c ArchiveCard) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	return nil
}

// RestoreCard undoes ArchiveCard.
type RestoreCard struct {
	Meta
	CardKey string
}

func (c RestoreCard) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	return nil
}

// AddComment appends a markdown comment to a card.
type AddComment struct {
	Meta
	CardKey string
	Body    string
}

func (c AddComment) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	if err := required("body", c.Body); err != nil {
		return err
	}
	return maxLen("body", c.Body, MaxComment)
}

// CreateLabel adds a project label.
type CreateLabel struct {
	Meta
	ProjectID string
	Name      string
	Color     string
}

func (c CreateLabel) Validate() error {
	if err := required("project_id", c.ProjectID); err != nil {
		return err
	}
	if err := required("name", c.Name); err != nil {
		return err
	}
	if err := maxLen("name", c.Name, MaxName); err != nil {
		return err
	}
	if !colorRe.MatchString(c.Color) {
		return invalid("color", "hex color like #FF8800")
	}
	return nil
}

// SetCardLabels replaces the label set of a card.
type SetCardLabels struct {
	Meta
	CardKey  string
	LabelIDs []string
}

func (c SetCardLabels) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	if len(c.LabelIDs) > 50 {
		return invalid("label_ids", "more than 50 labels")
	}
	return nil
}

// SetAssignees replaces the assignee set of a card.
type SetAssignees struct {
	Meta
	CardKey string
	UserIDs []string
}

func (c SetAssignees) Validate() error {
	if _, _, err := keys.ParseCard(c.CardKey); err != nil {
		return invalid("card_key", err.Error())
	}
	if len(c.UserIDs) > 50 {
		return invalid("user_ids", "more than 50 assignees")
	}
	return nil
}
