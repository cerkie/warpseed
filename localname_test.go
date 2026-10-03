package main

import (
	"strings"
	"testing"
)

func TestFitNameLeavesNormalNamesAlone(t *testing.T) {
	for _, n := range []string{"a.mkv", strings.Repeat("x", 255)} {
		if got, changed := fitName(n); got != n || changed {
			t.Fatalf("%q was changed", n)
		}
	}
}

func TestFitNameShortensAndKeepsTheExtension(t *testing.T) {
	long := strings.Repeat("Release.Name.", 25) + ".mkv"
	got, changed := fitName(long)
	if !changed || utf16Len(got) > maxLocalNameUnits || !strings.HasSuffix(got, ".mkv") {
		t.Fatalf("got %q (%d units)", got, utf16Len(got))
	}
	other, _ := fitName(long + "x")
	again, _ := fitName(long)
	if got != again || got == other {
		t.Fatal("shortening must be stable, and different for different names")
	}
}

func TestFitNameCountsUTF16Units(t *testing.T) {
	// Each of these is two UTF-16 units, so 200 of them is 400.
	long := strings.Repeat("\U0001F600", 200) + ".txt"
	got, changed := fitName(long)
	if !changed || utf16Len(got) > maxLocalNameUnits {
		t.Fatalf("%d units", utf16Len(got))
	}
}
