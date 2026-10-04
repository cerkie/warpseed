package main

import "testing"

func TestByteText(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 5 << 30: "5.0 GiB"} {
		if got := byteText(n); got != want {
			t.Errorf("byteText(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSpaceShortfall(t *testing.T) {
	if short, by := spaceShortfall(10, 4); !short || by != 6 {
		t.Errorf("10 into 4: %v %d", short, by)
	}
	for _, c := range [][2]int64{{4, 10}, {10, 10}, {0, 10}, {10, 0}, {-1, 5}} {
		if short, _ := spaceShortfall(c[0], c[1]); short {
			t.Errorf("%v should not warn", c)
		}
	}
}
