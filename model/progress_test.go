package model

import "testing"

func TestParseProgressLineRemainingTimeWithSpaces(t *testing.T) {
	prev := Progress{Remaining: "-"}
	line := "pct rescued: 12.3%, read errors: 2, remaining time: 1h 23m"

	p := ParseProgressLine(line, prev)

	if p.Remaining != "1h 23m" {
		t.Fatalf("remaining mismatch: got %q, want %q", p.Remaining, "1h 23m")
	}
}

func TestParseProgressLineKeepsPreviousRemainingWhenMissing(t *testing.T) {
	prev := Progress{Remaining: "9m 10s"}
	line := "current rate: 128 MB/s"

	p := ParseProgressLine(line, prev)

	if p.Remaining != "9m 10s" {
		t.Fatalf("remaining should be preserved: got %q, want %q", p.Remaining, "9m 10s")
	}
}
