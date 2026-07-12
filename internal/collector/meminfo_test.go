package collector

import "testing"

// meminfoSample is the real /proc/meminfo excerpt captured from the reference
// device (SPEC.md §6.1).
const meminfoSample = `MemTotal:        3016708 kB
MemFree:          134236 kB
MemAvailable:    1497592 kB
Buffers:           22760 kB
Cached:          1294048 kB
SwapCached:            0 kB
SwapTotal:        524284 kB
SwapFree:         524184 kB`

func TestParseMeminfo(t *testing.T) {
	got, err := parseMeminfo(meminfoSample)
	if err != nil {
		t.Fatalf("parseMeminfo returned error: %v", err)
	}
	want := map[string]float64{
		"MemTotal":     3016708 * 1024,
		"MemFree":      134236 * 1024,
		"MemAvailable": 1497592 * 1024,
		"Buffers":      22760 * 1024,
		"Cached":       1294048 * 1024,
		"SwapCached":   0, // parsed but not exported
		"SwapTotal":    524284 * 1024,
		"SwapFree":     524184 * 1024,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s: got %v, want %v", k, got[k], v)
		}
	}
}

func TestParseMeminfoEmpty(t *testing.T) {
	if _, err := parseMeminfo(""); err == nil {
		t.Fatal("expected error on empty input, got nil")
	}
	if _, err := parseMeminfo("garbage without kB units\nmore\n"); err == nil {
		t.Fatal("expected error when no kB lines present, got nil")
	}
}
