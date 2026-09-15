package tools_test

// go.mod replace guard (MVU5-5). Fails when go.mod carries a `replace`
// directive whose target is a relative or absolute filesystem path (starts
// with "." or "/"). A local-path replace makes the build depend on a sibling
// checkout: anyone building outside this workspace layout fails, and a
// release binary silently embeds whatever the sibling checkout holds. The
// directive was committed during MVU2-9 as a temporary bridge to unreleased
// provider client methods; it must not come back silently.
//
// This test is RED until terraform-provider-iaas v0.4.0 is tagged (owner
// action, punk /questions/MVU5-5: tag provider feature head 86d6be4) and the
// replace is swapped for the tag. Post-tag sequence:
//
//	go mod edit -dropreplace=github.com/hypervisor-io/terraform-provider-iaas
//	go get github.com/hypervisor-io/terraform-provider-iaas@v0.4.0
//	go mod tidy
//	grep 'terraform-provider-iaas v0.4.0' go.sum   # go.sum must pin the tag
//	tmp=$(mktemp -d) && git clone <repo-url> "$tmp/repo" && cd "$tmp/repo" \
//	  && go build ./... && go test ./...   # clean clone, NO sibling provider
//
// After the swap this test goes green and stays as the permanent guard. It
// runs in CI via the existing `go test -race ./...` step; no wiring needed.

import (
	"os"
	"strings"
	"testing"
)

// goModPath is the repo-root go.mod relative to this package's test working
// directory (internal/tools).
const goModPath = "../../go.mod"

func TestGoModReplaceGuard(t *testing.T) {
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading %s: %v", goModPath, err)
	}

	inReplaceBlock := false
	for lineNo, line := range strings.Split(string(raw), "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch {
		case fields[0] == "replace" && len(fields) == 1:
			t.Fatalf("%s:%d: bare `replace` keyword; expected `replace (` block or single-line form", goModPath, lineNo+1)
		case fields[0] == "replace" && fields[1] == "(":
			inReplaceBlock = true
		case fields[0] == "replace":
			checkReplaceTarget(t, goModPath, lineNo+1, fields[1:])
		case fields[0] == ")" && inReplaceBlock:
			inReplaceBlock = false
		case inReplaceBlock:
			checkReplaceTarget(t, goModPath, lineNo+1, fields)
		}
	}
}

// checkReplaceTarget inspects one replace entry (`old [v] => new [v]`) and
// fails the test when the replacement target is a filesystem path.
func checkReplaceTarget(t *testing.T, path string, lineNo int, fields []string) {
	t.Helper()
	for i, f := range fields {
		if f != "=>" || i+1 >= len(fields) {
			continue
		}
		target := fields[i+1]
		if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") {
			t.Fatalf(
				"%s:%d: replace directive targets a local filesystem path (%q); a local replace breaks builds outside this workspace and must not be committed.\nPin a tagged module version instead (see the docblock in gomod_replace_guard_test.go for the MVU5-5 swap sequence).",
				path, lineNo, target,
			)
		}
		return
	}
}
