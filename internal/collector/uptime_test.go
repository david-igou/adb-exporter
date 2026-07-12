package collector

import "testing"

func TestParseUptime(t *testing.T) {
	// Real /proc/uptime from the reference device (SPEC.md §6.3).
	got, err := parseUptime("2211.96 8352.28")
	if err != nil {
		t.Fatalf("parseUptime returned error: %v", err)
	}
	if got != 2211.96 {
		t.Errorf("got %v, want 2211.96", got)
	}
}

func TestParseUptimeErrors(t *testing.T) {
	for _, in := range []string{"", "notanumber rest"} {
		if _, err := parseUptime(in); err == nil {
			t.Errorf("expected error for %q, got nil", in)
		}
	}
}
