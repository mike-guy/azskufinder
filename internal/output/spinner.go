package output

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// A spinner is a few lines of code, so it is written here rather than taken
// from a library. That keeps it on the same Styles as the rest of the output,
// which spinner libraries replace with their own colour handling.

// Escape sequences the spinner writes.
const (
	clearLine  = "\r\x1b[K"
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 100 * time.Millisecond

// Spinner animates a one-line status message on a terminal while work runs.
type Spinner struct {
	w    io.Writer
	st   Styles
	msg  string
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// StartSpinner draws msg after an animated frame on w until Stop is called.
// The caller decides whether w is a terminal, and passes a msg that is safe to
// print and the Styles for w.
func StartSpinner(w io.Writer, st Styles, msg string) *Spinner {
	s := &Spinner{w: w, st: st, msg: msg, stop: make(chan struct{}), done: make(chan struct{})}

	go s.run()

	return s
}

// run draws the frames until stop is closed. It is the only writer to w until
// it closes done.
func (s *Spinner) run() {
	defer close(s.done)

	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	_, _ = fmt.Fprint(s.w, hideCursor)

	// The first frame is drawn at once, so there is no blank pause.
	for i := 0; ; i++ {
		_, _ = fmt.Fprint(s.w, "\r"+s.st.Bold(spinnerFrames[i%len(spinnerFrames)])+" "+s.st.Dim(s.msg)+"\x1b[K")

		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
	}
}

// Stop ends the animation, clears the line and shows the cursor again. Nothing
// is written after it returns. It is safe to call more than once, and on a nil
// *Spinner.
func (s *Spinner) Stop() {
	if s == nil {
		return
	}

	s.once.Do(func() {
		close(s.stop)
		<-s.done

		_, _ = fmt.Fprint(s.w, clearLine+showCursor)
	})
}
