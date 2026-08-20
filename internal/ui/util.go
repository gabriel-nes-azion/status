package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// width returns the rendered cell width of a plain string.
func width(s string) int { return lipgloss.Width(s) }

// truncate shortens s to at most n cells, adding an ellipsis when it cuts.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := width(string(r))
		if used+w > n-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// pad right-pads s with spaces to exactly n cells (truncating when longer).
func pad(s string, n int) string {
	s = truncate(s, n)
	if d := n - width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// padStyled right-pads an already-styled string to n cells. Unlike pad it never
// truncates, because cutting a string that carries ANSI escapes would slice
// through them; callers size the content before styling it.
func padStyled(s string, n int) string {
	if d := n - width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// lr composes two already-styled segments into exactly w cells, left and right
// aligned. The right segment is optional context, so it is dropped rather than
// allowed to collide with the left one.
func lr(left, right string, w int) string {
	lw, rw := width(left), width(right)
	if right == "" || lw+rw+1 > w {
		return padStyled(left, w)
	}
	return left + strings.Repeat(" ", w-lw-rw) + right
}

// wrapTokens packs a sep-separated list into at most maxLines lines of w cells,
// breaking at the separators so a detail line never splits mid-value.
//
// When the content does not fit, the final line falls back to a plain truncation
// of everything still unplaced. Packing whole tokens would instead drop the tail
// silently, and the dropped tail is usually the interesting part — "8 cores" is
// far less useful than "8 cores · load 2.31 1.9…".
func wrapTokens(s, sep string, w, maxLines int) []string {
	if w <= 0 || maxLines <= 0 {
		return nil
	}
	tokens := strings.Split(s, sep)

	var lines []string
	cur, curStart := "", 0
	for i, tok := range tokens {
		cand := tok
		if cur != "" {
			cand = cur + sep + tok
		}
		if width(cand) <= w {
			cur = cand
			continue
		}
		if len(lines) == maxLines-1 {
			// Last line available: show as much of the remainder as fits.
			return append(lines, truncate(strings.Join(tokens[curStart:], sep), w))
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		cur, curStart = truncate(tok, w), i
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// notice renders a short standalone message for the states where there is no
// room to draw the dashboard at all, keeping every line inside the terminal.
func notice(w int, lines ...string) string {
	out := make([]string, 0, len(lines)+2)
	out = append(out, "")
	for _, l := range lines {
		out = append(out, "  "+truncate(l, max(1, w-2)))
	}
	return strings.Join(append(out, ""), "\n")
}
