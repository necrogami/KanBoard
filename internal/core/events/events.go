// Package events defines the append-only activity log entries. Payloads
// carry ids, keys and names only; never emails, addresses or secrets
// (spec 4.3: facts survive, PII does not).
package events

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// Kind names what happened.
type Kind string

// ActorKind records whether a person, an agent through a token, or the
// system itself caused the event.
type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

const (
	WorkspaceCreated  Kind = "workspace.created"
	ProjectCreated    Kind = "project.created"
	ProjectUpdated    Kind = "project.updated"
	BoardCreated      Kind = "board.created"
	BoardUpdated      Kind = "board.updated"
	ColumnCreated     Kind = "column.created"
	ColumnUpdated     Kind = "column.updated"
	ColumnArchived    Kind = "column.archived"
	ColumnRestored    Kind = "column.restored"
	CardCreated       Kind = "card.created"
	CardUpdated       Kind = "card.updated"
	CardMoved         Kind = "card.moved"
	CardArchived      Kind = "card.archived"
	CardRestored      Kind = "card.restored"
	CommentAdded      Kind = "comment.added"
	CommentEdited     Kind = "comment.edited"
	CommentDeleted    Kind = "comment.deleted"
	LabelCreated      Kind = "label.created"
	LabelAdded        Kind = "label.added"
	LabelRemoved      Kind = "label.removed"
	AssigneeAdded     Kind = "assignee.added"
	AssigneeRemoved   Kind = "assignee.removed"
	MemberAdded       Kind = "member.added"
	MemberRemoved     Kind = "member.removed"
	MemberRoleChanged Kind = "member.role_changed"
)

var kinds = []Kind{
	WorkspaceCreated, ProjectCreated, ProjectUpdated, BoardCreated, BoardUpdated,
	ColumnCreated, ColumnUpdated, ColumnArchived, ColumnRestored,
	CardCreated, CardUpdated, CardMoved, CardArchived, CardRestored,
	CommentAdded, CommentEdited, CommentDeleted,
	LabelCreated, LabelAdded, LabelRemoved, AssigneeAdded, AssigneeRemoved,
	MemberAdded, MemberRemoved, MemberRoleChanged,
}

// Kinds returns every known kind.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// Known reports whether k is a defined kind.
func Known(k Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Event is one activity log row.
type Event struct {
	ID          string
	WorkspaceID string
	ProjectID   string
	BoardID     string
	CardID      string
	Seq         int64
	ActorUserID string
	ViaTokenID  string
	ActorKind   ActorKind
	Kind        Kind
	Payload     string
	OccurredAt  time.Time
}

// New builds an Event of kind with payload encoded as JSON. Ids, actor
// and seq are filled by the service layer.
func New(kind Kind, payload any) (Event, error) {
	if !Known(kind) {
		return Event{}, fmt.Errorf("events: unknown kind %q", kind)
	}
	if payload == nil {
		payload = struct{}{}
	} else if v := reflect.ValueOf(payload); v.Kind() == reflect.Pointer && v.IsNil() {
		payload = struct{}{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("events: encode %s: %w", kind, err)
	}
	return Event{Kind: kind, Payload: string(b)}, nil
}

// Payload structs. Field names are part of the webhook and SSE contract.
//
// What user content a payload may carry, since the log is append-only
// and survives account deletion by design (spec 4.3): a payload carries
// ids, and the short display names a reader needs to render an activity
// entry without a lookup (a card title, a column name, a label name).
// It never carries a large free-text field: a change to one records
// only that the field changed. Card titles are therefore retained in
// the log verbatim, and a title can contain personal data, which is a
// deliberate trade for a legible history.

type CardCreatedPayload struct {
	Number     int64  `json:"number"`
	Title      string `json:"title"`
	ColumnID   string `json:"column_id"`
	ColumnName string `json:"column_name"`
}

// CardUpdatedPayload carries the old and new value of a short field and
// empty strings for a large one; see the rule above the payload structs.
type CardUpdatedPayload struct {
	Field string `json:"field"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

type CardMovedPayload struct {
	FromColumnID string `json:"from_column_id"`
	ToColumnID   string `json:"to_column_id"`
	FromName     string `json:"from_name"`
	ToName       string `json:"to_name"`
	FromCategory string `json:"from_category"`
	ToCategory   string `json:"to_category"`
}

type CommentPayload struct {
	CommentID string `json:"comment_id"`
}

type LabelPayload struct {
	LabelID string `json:"label_id"`
	Name    string `json:"name"`
}

type AssigneePayload struct {
	UserID string `json:"user_id"`
}

type ColumnPayload struct {
	ColumnID string `json:"column_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type BoardPayload struct {
	Name string `json:"name"`
}

type ProjectPayload struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type MemberPayload struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}
