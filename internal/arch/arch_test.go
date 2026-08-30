package arch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/necrogami/kanboard"

// rule is either a denylist or an allowlist. forbidden bans a package
// and everything that depends on it transitively. allowed is the
// opposite shape: of the module's own packages, only these (and the
// roots themselves) may be imported directly. Spec 3's ui rule is an
// allowlist, so it must not be written as a denylist of the packages
// that happen to exist today.
type rule struct {
	roots     []string
	forbidden string
	allowed   []string
}

var rules = []rule{
	{roots: []string{"./internal/core/...", "./internal/client/...", "./internal/i18n/...", "./ui/..."}, forbidden: module + "/internal/server"},
	{roots: []string{"./internal/server/api/...", "./internal/server/mcp/...", "./internal/server/web/..."}, forbidden: module + "/internal/server/store"},
	{roots: []string{"./ui/..."}, allowed: []string{module + "/ui", module + "/internal/client/viewmodel", module + "/internal/i18n"}},
}

func TestImportRules(t *testing.T) {
	root := repoRoot(t)
	for _, r := range rules {
		existing := existingRoots(t, root, r.roots)
		if len(existing) == 0 {
			continue
		}
		if r.forbidden != "" {
			// -deps: a forbidden package must not be reachable at all.
			for _, line := range strings.Split(goList(t, root, existing, "-deps", "{{.ImportPath}}"), "\n") {
				if strings.HasPrefix(line, r.forbidden) {
					t.Errorf("%v must not depend on %s (found %s)", existing, r.forbidden, line)
				}
			}
		}
		if len(r.allowed) > 0 {
			// Direct imports only: the allowed packages bring their own
			// dependencies with them, and those are their business.
			for _, line := range strings.Split(goList(t, root, existing, "", `{{.ImportPath}}{{range .Imports}} {{.}}{{end}}`), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				for _, imp := range fields[1:] {
					if !importAllowed(imp, r.allowed) {
						t.Errorf("%s imports %s; %v may import only %v", fields[0], imp, existing, r.allowed)
					}
				}
			}
		}
	}
}

// importAllowed reports whether imp is acceptable under an allowlist.
// Packages outside this module are not the rule's business; the
// depguard config in .golangci.yml is stricter and names them too.
func importAllowed(imp string, allowed []string) bool {
	if !strings.HasPrefix(imp, module+"/") {
		return true
	}
	for _, a := range allowed {
		if imp == a || strings.HasPrefix(imp, a+"/") {
			return true
		}
	}
	return false
}

// TestImportAllowedRule pins the allowlist logic itself, so the ui rule
// is known to work before ui/ exists rather than the first time someone
// breaks it.
func TestImportAllowedRule(t *testing.T) {
	allowed := []string{module + "/ui", module + "/internal/client/viewmodel", module + "/internal/i18n"}
	for _, imp := range []string{"strings", "github.com/example/toolkit", module + "/ui/widget", module + "/internal/i18n", module + "/internal/client/viewmodel/board"} {
		if !importAllowed(imp, allowed) {
			t.Errorf("importAllowed(%q) = false, want true", imp)
		}
	}
	for _, imp := range []string{module + "/internal/client/cache", module + "/internal/server/store", module + "/internal/core/order", module + "/internal/i18nx"} {
		if importAllowed(imp, allowed) {
			t.Errorf("importAllowed(%q) = true, want false", imp)
		}
	}
}

func existingRoots(t *testing.T, root string, roots []string) []string {
	t.Helper()
	var out []string
	for _, p := range roots {
		dir := filepath.Join(root, strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/..."))
		if _, err := os.Stat(dir); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func goList(t *testing.T, root string, pkgs []string, flag, format string) string {
	t.Helper()
	args := []string{"list"}
	if flag != "" {
		args = append(args, flag)
	}
	args = append(args, "-f", format)
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", pkgs, err, out)
	}
	return string(out)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
