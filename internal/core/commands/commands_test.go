package commands_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/commands"
)

func field(t *testing.T, err error, want string) {
	t.Helper()
	var ve *commands.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if ve.Field != want {
		t.Fatalf("field = %q, want %q", ve.Field, want)
	}
}

func TestCreateProject(t *testing.T) {
	ok := commands.CreateProject{WorkspaceID: "w", Key: "PROJ", Name: "Project"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	field(t, commands.CreateProject{WorkspaceID: "w", Key: "p", Name: "x"}.Validate(), "key")
	field(t, commands.CreateProject{WorkspaceID: "w", Key: "PROJ", Name: ""}.Validate(), "name")
	field(t, commands.CreateProject{Key: "PROJ", Name: "x"}.Validate(), "workspace_id")
	field(t, commands.CreateProject{WorkspaceID: "w", Key: "PROJ", Name: strings.Repeat("x", 201)}.Validate(), "name")
}

func TestCreateCard(t *testing.T) {
	field(t, commands.CreateCard{ProjectID: "p", Title: ""}.Validate(), "title")
	field(t, commands.CreateCard{ProjectID: "p", Title: strings.Repeat("x", 501)}.Validate(), "title")
	field(t, commands.CreateCard{ProjectID: "p", Title: "ok", Description: strings.Repeat("x", 100001)}.Validate(), "description")
	field(t, commands.CreateCard{Title: "ok"}.Validate(), "project_id")
	if err := (commands.CreateCard{ProjectID: "p", Title: "  ok  "}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveCard(t *testing.T) {
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", AfterKey: "PROJ-2", BeforeKey: "PROJ-3"}.Validate(), "before_key")
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", BeforeKey: "PROJ-2", Position: commands.PositionTop}.Validate(), "position")
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", Position: "middle"}.Validate(), "position")
	field(t, commands.MoveCard{CardKey: "nope", ColumnID: "c"}.Validate(), "card_key")
	field(t, commands.MoveCard{CardKey: "PROJ-1"}.Validate(), "column_id")
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", AfterKey: "nope"}.Validate(), "after_key")
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", BeforeKey: "nope"}.Validate(), "before_key")
	if err := (commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", Position: commands.PositionTop}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCardNeedsAChange(t *testing.T) {
	field(t, commands.UpdateCard{CardKey: "PROJ-1"}.Validate(), "patch")
	field(t, commands.UpdateCard{CardKey: "nope"}.Validate(), "card_key")

	empty := ""
	field(t, commands.UpdateCard{CardKey: "PROJ-1", Title: &empty}.Validate(), "title")

	longTitle := strings.Repeat("x", 501)
	field(t, commands.UpdateCard{CardKey: "PROJ-1", Title: &longTitle}.Validate(), "title")

	longDesc := strings.Repeat("x", 100001)
	field(t, commands.UpdateCard{CardKey: "PROJ-1", Description: &longDesc}.Validate(), "description")

	due := time.Now()
	field(t, commands.UpdateCard{CardKey: "PROJ-1", DueDate: &due, ClearDueDate: true}.Validate(), "due_date")

	title := "new"
	if err := (commands.UpdateCard{CardKey: "PROJ-1", Title: &title}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateLabel(t *testing.T) {
	field(t, commands.CreateLabel{ProjectID: "p", Name: "bug", Color: "red"}.Validate(), "color")
	field(t, commands.CreateLabel{Name: "bug", Color: "#FF0000"}.Validate(), "project_id")
	field(t, commands.CreateLabel{ProjectID: "p", Name: "", Color: "#FF0000"}.Validate(), "name")
	field(t, commands.CreateLabel{ProjectID: "p", Name: strings.Repeat("x", 201), Color: "#FF0000"}.Validate(), "name")
	if err := (commands.CreateLabel{ProjectID: "p", Name: "bug", Color: "#FF0000"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWorkspace(t *testing.T) {
	field(t, commands.CreateWorkspace{Name: "x", Slug: "Bad Slug", AdminEmail: "a@b.c", AdminName: "A"}.Validate(), "slug")
	field(t, commands.CreateWorkspace{Name: "x", Slug: "ok", AdminEmail: "nope", AdminName: "A"}.Validate(), "admin_email")
	field(t, commands.CreateWorkspace{Name: "", Slug: "ok", AdminEmail: "a@b.c", AdminName: "A"}.Validate(), "name")
	field(t, commands.CreateWorkspace{Name: "x", Slug: "ok", AdminEmail: "a@b.c", AdminName: ""}.Validate(), "admin_name")
	if err := (commands.CreateWorkspace{Name: "x", Slug: "ok-1", AdminEmail: "a@b.c", AdminName: "A"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveCard(t *testing.T) {
	field(t, commands.ArchiveCard{CardKey: "nope"}.Validate(), "card_key")
	if err := (commands.ArchiveCard{CardKey: "PROJ-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreCard(t *testing.T) {
	field(t, commands.RestoreCard{CardKey: "nope"}.Validate(), "card_key")
	if err := (commands.RestoreCard{CardKey: "PROJ-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAddComment(t *testing.T) {
	field(t, commands.AddComment{CardKey: "nope", Body: "x"}.Validate(), "card_key")
	field(t, commands.AddComment{CardKey: "PROJ-1", Body: ""}.Validate(), "body")
	field(t, commands.AddComment{CardKey: "PROJ-1", Body: strings.Repeat("x", 20001)}.Validate(), "body")
	if err := (commands.AddComment{CardKey: "PROJ-1", Body: "looks good"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSetCardLabels(t *testing.T) {
	field(t, commands.SetCardLabels{CardKey: "nope"}.Validate(), "card_key")
	field(t, commands.SetCardLabels{CardKey: "PROJ-1", LabelIDs: make([]string, 51)}.Validate(), "label_ids")
	if err := (commands.SetCardLabels{CardKey: "PROJ-1", LabelIDs: []string{"l1", "l2"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSetAssignees(t *testing.T) {
	field(t, commands.SetAssignees{CardKey: "nope"}.Validate(), "card_key")
	field(t, commands.SetAssignees{CardKey: "PROJ-1", UserIDs: make([]string, 51)}.Validate(), "user_ids")
	if err := (commands.SetAssignees{CardKey: "PROJ-1", UserIDs: []string{"u1", "u2"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
