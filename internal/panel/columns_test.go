package panel

import (
	"io"
	"slices"
	"strings"
	"testing"
)

func TestFitColumnsKeepsTheMoreImportantColumnsFirst(t *testing.T) {
	cols := []column{{min: 10, want: 20}, {min: 10, want: 10}, {min: 10, want: 10}}
	for _, tc := range []struct {
		w    int
		want []int
	}{
		{10, []int{10, 0, 0}},
		{20, []int{20, 0, 0}},
		{32, []int{20, 10, 0}},
		{44, []int{20, 10, 10}},
		{60, []int{20, 10, 10}},
	} {
		if got := fitColumns(tc.w, 2, cols); !slices.Equal(got, tc.want) {
			t.Errorf("fitColumns(%d) = %v, want %v", tc.w, got, tc.want)
		}
	}
}

func TestFitColumnsStopsAtTheFirstColumnThatDoesNotFit(t *testing.T) {
	// The second column needs 10 cells with its gap, so the third (which would fit
	// after the first) is not tried.
	cols := []column{{min: 5, want: 5}, {min: 9, want: 9}, {min: 3, want: 3}}
	if got := fitColumns(12, 1, cols); !slices.Equal(got, []int{5, 0, 0}) {
		t.Errorf("fitColumns(12) = %v, want [5 0 0]", got)
	}
}

func TestFitColumnsFlexTakesWhatIsLeft(t *testing.T) {
	cols := []column{{min: 5, want: 5}, {min: 4, want: 4, flex: true}, {min: 3, want: 3}}
	if got := fitColumns(30, 2, cols); !slices.Equal(got, []int{5, 18, 3}) {
		t.Errorf("fitColumns(30) = %v, want [5 18 3]", got)
	}
}

func TestFitColumnsLeavesOutAnAbsentColumnWithoutAGap(t *testing.T) {
	cols := []column{{min: 4, want: 4}, {}, {min: 4, want: 4}}
	if got := fitColumns(10, 2, cols); !slices.Equal(got, []int{4, 0, 4}) {
		t.Errorf("fitColumns(10) = %v, want [4 0 4]", got)
	}
}

func TestCellSegsSkipsEmptyCellsAndAligns(t *testing.T) {
	base := newStyles(newRenderer(io.Discard, false)).base
	segs := cellSegs([]cell{
		{text: "a", style: base, width: 3},
		{text: "x", style: base, width: 0},
		{text: "b", style: base, width: 4, right: true},
	}, 1, base)
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	if want := "a  " + " " + "   b"; b.String() != want {
		t.Errorf("cells = %q, want %q", b.String(), want)
	}
}
