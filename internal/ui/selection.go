package ui

import tea "github.com/charmbracelet/bubbletea"

// selection is the cursor and marked set behind the modal lists that let you
// tick some rows and then act on them: /discover picks processes to chart,
// /service picks services to stop charting. Sharing the mechanics is what keeps
// the two from drifting apart on wrapping, clamping and what Enter means.
//
// The /show picker deliberately does not use this: it applies each toggle
// immediately, so it has no marked set to confirm.
type selection struct {
	idx    int
	marked map[string]bool
}

func newSelection() selection { return selection{marked: map[string]bool{}} }

// clamp keeps the cursor inside a list whose length may have changed underneath.
func (s *selection) clamp(n int) {
	if n <= 0 {
		s.idx = 0
		return
	}
	if s.idx >= n {
		s.idx = n - 1
	}
	if s.idx < 0 {
		s.idx = 0
	}
}

// move steps the cursor, wrapping at both ends.
func (s *selection) move(delta, n int) {
	if n <= 0 {
		s.idx = 0
		return
	}
	s.idx = ((s.idx+delta)%n + n) % n
}

// toggle flips one key's mark.
func (s *selection) toggle(key string) {
	if s.marked == nil {
		s.marked = map[string]bool{}
	}
	if s.marked[key] {
		delete(s.marked, key)
		return
	}
	s.marked[key] = true
}

func (s *selection) mark(key string) {
	if s.marked == nil {
		s.marked = map[string]bool{}
	}
	s.marked[key] = true
}

func (s selection) isMarked(key string) bool { return s.marked[key] }
func (s selection) count() int               { return len(s.marked) }

func (s *selection) reset() {
	s.idx = 0
	s.marked = map[string]bool{}
}

// listAction is what a keypress means to a select-then-confirm list.
type listAction int

const (
	listNone    listAction = iota
	listQuit               // ctrl+c: leave the program
	listCancel             // esc or q: close, changing nothing
	listConfirm            // enter: act on the marked rows
	listToggle             // space or x: mark the row under the cursor
)

// classify maps a keypress onto the shared list vocabulary, so every such list
// answers to the same keys.
func classify(k tea.KeyMsg) listAction {
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return listQuit
	case tea.KeyEsc:
		return listCancel
	case tea.KeyEnter:
		return listConfirm
	case tea.KeySpace:
		return listToggle
	case tea.KeyUp, tea.KeyDown:
		return listNone // handled by the caller, which knows the length
	}
	switch k.String() {
	case "x", "X":
		return listToggle
	case "q", "Q":
		return listCancel
	}
	return listNone
}

// cursorDelta is the movement a keypress asks for, zero when it asks for none.
func cursorDelta(k tea.KeyMsg) int {
	switch k.Type {
	case tea.KeyUp:
		return -1
	case tea.KeyDown:
		return 1
	}
	switch k.String() {
	case "k", "K":
		return -1
	case "j", "J":
		return 1
	}
	return 0
}
