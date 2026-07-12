package collector

import "testing"

func TestParseLoadavg(t *testing.T) {
	// Real /proc/loadavg from the reference device (SPEC.md §6.2).
	got, err := parseLoadavg("0.02 0.10 0.19 1/1498 9159")
	if err != nil {
		t.Fatalf("parseLoadavg returned error: %v", err)
	}
	want := [3]float64{0.02, 0.10, 0.19}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseLoadavgErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"too few fields", "0.02 0.10"},
		{"non-numeric", "a b c 1/2 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseLoadavg(tc.in); err == nil {
				t.Errorf("expected error for %q, got nil", tc.in)
			}
		})
	}
}
