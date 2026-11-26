package craft

import (
	"testing"
	"unicode/utf8"
)

func TestSearchFold(t *testing.T) {
	if utf8.RuneCountInString(accented) != utf8.RuneCountInString(plain) {
		t.Fatal("accented and plain must pair letter by letter")
	}
	if got := searchFold("Mouliné SPÉCIAL Čerň ёлка"); got != "mouline special cern елка" {
		t.Errorf("searchFold = %q", got)
	}
}
