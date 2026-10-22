package output

import (
	"bytes"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

// syncBuffer is a bytes.Buffer the test can read while the spinner's goroutine
// writes to it. synctest.Wait does not order the goroutine's earlier writes
// before the test's reads, so the race detector needs the mutex.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// spinnerFrame is what run draws for frame i.
func spinnerFrame(st Styles, i int, msg string) string {
	return "\r" + st.Bold(spinnerFrames[i%len(spinnerFrames)]) + " " + st.Dim(msg) + "\x1b[K"
}

func TestSpinner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		styles Styles
		ticks  int
	}{
		{name: "colour off", styles: Styles{}, ticks: 2},
		{name: "colour on", styles: NewStyles(true), ticks: 1},
		{name: "frames wrap", styles: Styles{}, ticks: len(spinnerFrames)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				const msg = "Finding SKUs"

				var buf syncBuffer

				s := StartSpinner(&buf, tc.styles, msg)
				synctest.Wait()

				want := hideCursor + spinnerFrame(tc.styles, 0, msg)
				if diff := cmp.Diff(want, buf.String()); diff != "" {
					t.Fatalf("after start, output mismatch (-want +got):\n%s", diff)
				}

				for i := 1; i <= tc.ticks; i++ {
					time.Sleep(spinnerInterval)
					synctest.Wait()

					want += spinnerFrame(tc.styles, i, msg)
					if diff := cmp.Diff(want, buf.String()); diff != "" {
						t.Fatalf("after tick %d, output mismatch (-want +got):\n%s", i, diff)
					}
				}

				s.Stop()

				want += clearLine + showCursor
				if diff := cmp.Diff(want, buf.String()); diff != "" {
					t.Errorf("after Stop, output mismatch (-want +got):\n%s", diff)
				}
			})
		})
	}
}

func TestSpinner_Stop(t *testing.T) {
	t.Parallel()

	t.Run("nil spinner", func(t *testing.T) {
		t.Parallel()

		var s *Spinner
		s.Stop()
	})

	t.Run("nothing written after stop", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			var buf syncBuffer

			s := StartSpinner(&buf, Styles{}, "msg")
			synctest.Wait()

			s.Stop()
			want := buf.String()

			s.Stop()
			time.Sleep(3 * spinnerInterval)
			synctest.Wait()

			if diff := cmp.Diff(want, buf.String()); diff != "" {
				t.Errorf("output changed after Stop (-want +got):\n%s", diff)
			}
		})
	})
}
