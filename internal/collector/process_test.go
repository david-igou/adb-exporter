package collector

import "testing"

// psSample is the real `ps -A -o PID,RSS,%CPU,NAME` output from the reference
// device (SPEC.md §6.4), including the header and a zero-RSS kernel thread.
const psSample = `   PID    RSS %CPU NAME
     1   9060  0.2 init
  3699 333380  3.6 system_server
  4774 299252  0.1 com.google.android.gms
  4316 298388  0.3 com.google.android.gms.persistent
  4751 253592  6.1 com.spocky.projengmenu
  6477 168372  0.0 com.android.vending:background
     2      0  0.0 [kthreadd]`

func TestParsePS(t *testing.T) {
	got, err := parsePS(psSample)
	if err != nil {
		t.Fatalf("parsePS returned error: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d rows, want 7", len(got))
	}
	// Spot-check first, a colon name, and the kernel thread.
	if got[0] != (processInfo{PID: 1, RSSkB: 9060, CPUPerc: 0.2, Name: "init"}) {
		t.Errorf("row0 = %+v", got[0])
	}
	if got[5] != (processInfo{PID: 6477, RSSkB: 168372, CPUPerc: 0.0, Name: "com.android.vending:background"}) {
		t.Errorf("row5 = %+v", got[5])
	}
	if got[6] != (processInfo{PID: 2, RSSkB: 0, CPUPerc: 0.0, Name: "[kthreadd]"}) {
		t.Errorf("row6 = %+v", got[6])
	}
}

func TestParsePSEmpty(t *testing.T) {
	// Only a header ⇒ zero rows ⇒ error.
	if _, err := parsePS("   PID    RSS %CPU NAME"); err == nil {
		t.Fatal("expected error when only header present, got nil")
	}
}

func names(procs []processInfo) []string {
	out := make([]string, len(procs))
	for i, p := range procs {
		out[i] = p.Name
	}
	return out
}

func TestSelectProcessesTopN(t *testing.T) {
	procs, err := parsePS(psSample)
	if err != nil {
		t.Fatal(err)
	}
	c := newProcessCollector(3, nil)
	got := names(c.selectProcesses(procs))
	// Top 3 by RSS: system_server(333380), com.google.android.gms(299252),
	// com.google.android.gms.persistent(298388).
	want := []string{"system_server", "com.google.android.gms", "com.google.android.gms.persistent"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSelectProcessesAllowlistOutsideTopN(t *testing.T) {
	procs, err := parsePS(psSample)
	if err != nil {
		t.Fatal(err)
	}
	// top-1 is system_server; allowlist a zero-RSS kernel thread and a low-RSS
	// process to prove they are added despite falling outside top-N.
	c := newProcessCollector(1, []string{"[kthreadd]", "init"})
	got := names(c.selectProcesses(procs))
	want := []string{"system_server", "init", "[kthreadd]"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSelectProcessesDedupeByName(t *testing.T) {
	// Two rows share a NAME; only the highest-RSS instance survives.
	procs := []processInfo{
		{PID: 10, RSSkB: 100, Name: "gms"},
		{PID: 11, RSSkB: 500, Name: "gms"},
		{PID: 12, RSSkB: 300, Name: "other"},
	}
	c := newProcessCollector(15, nil)
	got := c.selectProcesses(procs)
	if len(got) != 2 {
		t.Fatalf("got %d procs, want 2 (deduped)", len(got))
	}
	for _, p := range got {
		if p.Name == "gms" && p.RSSkB != 500 {
			t.Errorf("gms kept RSS %d, want 500 (highest)", p.RSSkB)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
