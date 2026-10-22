package output

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>.golden, or rewrites the file when
// the tests run with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run with -update to create it)", path, err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
	}
}

var errWrite = errors.New("write failed")

// errWriter fails every write with errWrite.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errWrite }
