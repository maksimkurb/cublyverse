package craft

import (
	"slices"
	"testing"
)

func TestByColorOrdersByHowCloseColoursLook(t *testing.T) {
	target, ok := parseColor("#CC1020")
	if !ok {
		t.Fatal("parseColor rejected a valid value")
	}
	values := []string{"#FFFFFF", "", "#1030C0", "#D02030", "nonsense", "#701018", "#CC1020"}
	got := byColor(values, target, func(v string) string { return v })
	want := []string{"#CC1020", "#D02030", "#701018"}
	if len(got) != 5 || !slices.Equal(got[:3], want) {
		t.Errorf("byColor = %v, want five coloured values starting with %v", got, want)
	}
	if page := window(got, 2, 4); len(page) != 1 {
		t.Errorf("window past the end = %v, want one value", page)
	}
	if page := window(got, 2, 40); len(page) != 0 {
		t.Errorf("window beyond the list = %v, want nothing", page)
	}
}
