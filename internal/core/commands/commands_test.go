package commands_test

import (
	"errors"
	"strings"
	"testing"

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
}

func TestCreateCard(t *testing.T) {
	field(t, commands.CreateCard{ProjectID: "p", Title: ""}.Validate(), "title")
	field(t, commands.CreateCard{ProjectID: "p", Title: strings.Repeat("x", 501)}.Validate(), "title")
	field(t, commands.CreateCard{ProjectID: "p", Title: "ok", Description: strings.Repeat("x", 100001)}.Validate(), "description")
	if err := (commands.CreateCard{ProjectID: "p", Title: "  ok  "}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveCard(t *testing.T) {
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", AfterKey: "PROJ-2", BeforeKey: "PROJ-3"}.Validate(), "after_key")
	field(t, commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", Position: "middle"}.Validate(), "position")
	field(t, commands.MoveCard{CardKey: "nope", ColumnID: "c"}.Validate(), "card_key")
	if err := (commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (commands.MoveCard{CardKey: "PROJ-1", ColumnID: "c", Position: commands.PositionTop}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCardNeedsAChange(t *testing.T) {
	field(t, commands.UpdateCard{CardKey: "PROJ-1"}.Validate(), "patch")
	title := "new"
	if err := (commands.UpdateCard{CardKey: "PROJ-1", Title: &title}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateLabel(t *testing.T) {
	field(t, commands.CreateLabel{ProjectID: "p", Name: "bug", Color: "red"}.Validate(), "color")
	if err := (commands.CreateLabel{ProjectID: "p", Name: "bug", Color: "#FF0000"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWorkspace(t *testing.T) {
	field(t, commands.CreateWorkspace{Name: "x", Slug: "Bad Slug", AdminEmail: "a@b.c", AdminName: "A"}.Validate(), "slug")
	field(t, commands.CreateWorkspace{Name: "x", Slug: "ok", AdminEmail: "nope", AdminName: "A"}.Validate(), "admin_email")
	if err := (commands.CreateWorkspace{Name: "x", Slug: "ok-1", AdminEmail: "a@b.c", AdminName: "A"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
