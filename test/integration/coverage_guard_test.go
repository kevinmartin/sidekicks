package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoverageGateRejectsMalformedRecords verifies the coverage parser rejects invented source records.
func TestCoverageGateRejectsMalformedRecords(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"internal/compute/control/control.go", "internal/compute/runtime/docker/docker.go", "internal/compute/guest/server.go", "internal/compute/browser/browser.go", "internal/compute/neko/client.go"}
	var valid strings.Builder
	valid.WriteString("mode: atomic\n")
	for _, file := range files {
		valid.WriteString("github.com/kevinmartin/sidekicks/" + file + ":1.1,1.2 1 1\n")
	}
	for _, tc := range []struct {
		name, extra string
		valid       bool
	}{
		{name: "valid", valid: true},
		{name: "missing-range", extra: "github.com/kevinmartin/sidekicks/internal/compute/control/control.go 1 1\n"},
		{name: "foreign-package", extra: "example.org/internal/compute/control/control.go:1.1,1.2 1 1\n"},
		{name: "nonexistent-file", extra: "github.com/kevinmartin/sidekicks/internal/compute/control/fake.go:1.1,1.2 1 1\n"},
		{name: "traversal", extra: "github.com/kevinmartin/sidekicks/internal/../control.go:1.1,1.2 1 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := filepath.Join(t.TempDir(), "coverage.out")
			if err := os.WriteFile(profile, []byte(valid.String()+tc.extra), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "scripts/compute/check-coverage.sh", profile, "100")
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.valid {
				t.Fatalf("coverage gate: %v\n%s", err, output)
			}
		})
	}
}
