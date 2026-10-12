package panel

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tableGap is the space between the columns of a table.
const tableGap = 2

// column describes one column of a table row, in cells. A column is kept while it
// fits at min, and grows toward want as soon as it is kept, so a more important
// column is full before a less important one is squeezed. A min of 0 leaves it out.
// The flex column takes the room that the other columns leave, but no more than max.
type column struct {
	min, want, max int
	flex           bool
}

// fitColumns sizes a row of cols w cells wide, with gap cells between the columns it
// keeps. The columns come in priority order, most important first. A column is kept
// only if every column before it is, so a narrow row drops its least important
// columns, which are the last in the list. The flex column is the first one that is
// kept, and it grows last. It returns each column's width in the same order, 0 where
// the column is left out.
func fitColumns(w, gap int, cols []column) []int {
	widths := make([]int, len(cols))
	used, flex := 0, -1
	for i, c := range cols {
		if c.min <= 0 {
			continue
		}
		need := c.min
		if used > 0 {
			need += gap
		}
		if used+need > w {
			break
		}
		used += need
		widths[i] = c.min
		grow := min(max(c.want-c.min, 0), w-used)
		widths[i] += grow
		used += grow
		if c.flex && flex < 0 {
			flex = i
		}
	}
	if flex >= 0 {
		top := max(cols[flex].max, cols[flex].want)
		widths[flex] += min(max(top-widths[flex], 0), w-used)
	}
	return widths
}

// sizedCol is the column for cells: as wide as the widest of them, at most most, and
// at least least. The header under head counts toward the width. A column whose cells
// are all empty is left out.
func sizedCol(head string, cells []string, least, most int, flex bool) column {
	longest := 0
	for _, c := range cells {
		longest = max(longest, displayWidth(c))
	}
	if longest == 0 {
		return column{}
	}
	want := min(max(longest, displayWidth(head)), most)
	// max is the content width too: a flex column grows to fit its widest cell, never
	// past it, so no gap opens before the next column.
	return column{min: min(least, want), want: want, max: want, flex: flex}
}

// cell is the text of one column of a row, in its style. A cell of width 0 is left out.
type cell struct {
	text  string
	style lipgloss.Style
	width int
	right bool // right-aligns the text in its width
}

// cellSegs lays out cells from left to right, each fitted to its width. Cells of
// width 0 are left out, and the others are gap cells apart, drawn in gapStyle.
func cellSegs(cells []cell, gap int, gapStyle lipgloss.Style) []seg {
	var segs []seg
	for _, c := range cells {
		if c.width <= 0 {
			continue
		}
		if len(segs) > 0 {
			segs = append(segs, seg{strings.Repeat(" ", gap), gapStyle})
		}
		text := fit(c.text, c.width)
		if c.right {
			text = rjust(c.text, c.width)
		}
		segs = append(segs, seg{text, c.style})
	}
	return segs
}
