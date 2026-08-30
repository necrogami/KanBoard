package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// TestCardColumnsMatchesTheStruct guards the one hand-written scan in
// the package: cardColumns names card's columns in the order the
// rows.Scan in SearchCards expects, which is the field order of
// sqlitegen.Card. A generated field arriving without a matching column
// would otherwise scan values into the wrong fields.
func TestCardColumnsMatchesTheStruct(t *testing.T) {
	want := reflect.TypeOf(sqlitegen.Card{}).NumField()
	got := strings.Count(cardColumns, ",") + 1
	if got != want {
		t.Fatalf("cardColumns lists %d columns but sqlitegen.Card has %d fields; keep them in the same order", got, want)
	}
}
