package runner

import (
	"testing"

	"diskman-web/model"
)

func TestParseDdProgressComputesRemaining(t *testing.T) {
	line := "200 bytes copied, 2.0 s, 100 B/s"
	p := parseDdProgress(line, model.Progress{}, 1000)

	if p.Remaining != "8s" {
		t.Fatalf("remaining mismatch: got %q, want %q", p.Remaining, "8s")
	}
	if p.Percent != 20 {
		t.Fatalf("percent mismatch: got %.1f, want 20.0", p.Percent)
	}
}

func TestParseDdProgressDoneSetsZeroRemaining(t *testing.T) {
	line := "1000 bytes copied, 10.0 s, 100 B/s"
	p := parseDdProgress(line, model.Progress{}, 1000)

	if p.Remaining != "0s" {
		t.Fatalf("remaining mismatch: got %q, want %q", p.Remaining, "0s")
	}
}
