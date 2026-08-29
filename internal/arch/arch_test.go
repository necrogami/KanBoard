package arch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/necrogami/kanboard"

type rule struct {
	roots     []string
	forbidden string
}

var rules = []rule{
	{roots: []string{"./internal/core/...", "./internal/client/...", "./internal/i18n/...", "./ui/..."}, forbidden: module + "/internal/server"},
	{roots: []string{"./internal/server/api/...", "./internal/server/mcp/...", "./internal/server/web/..."}, forbidden: module + "/internal/server/store"},
	{roots: []string{"./ui/..."}, forbidden: module + "/internal/client/cache"},
}

func TestImportRules(t *testing.T) {
	root := repoRoot(t)
	for _, r := range rules {
		var existing []string
		for _, p := range r.roots {
			dir := filepath.Join(root, strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/..."))
			if _, err := os.Stat(dir); err == nil {
				existing = append(existing, p)
			}
		}
		if len(existing) == 0 {
			continue
		}
		args := append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, existing...)
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go list %v: %v\n%s", existing, err, out)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, r.forbidden) {
				t.Errorf("%v must not depend on %s (found %s)", existing, r.forbidden, line)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
