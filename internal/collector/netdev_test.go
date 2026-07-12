package collector

import "testing"

// netdevSample is /proc/net/dev with the two header lines and three complete
// 16-counter interface rows. The eth0 row uses the real captured values from
// SPEC.md §6.6; wlan0 and lo rows are shown in full (the spec excerpt truncated
// them).
const netdevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 4653697    8110    0    0    0     0          0       241  1847905    7085    0    0    0     0       0          0
 wlan0:       0       0    0    0    0     0          0         0        0       0    0    0    0     0       0          0
    lo:    9388      71    0    0    0     0          0         0     9388      71    0    0    0     0       0          0`

func TestParseNetdev(t *testing.T) {
	got, err := parseNetdev(netdevSample)
	if err != nil {
		t.Fatalf("parseNetdev returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d interfaces, want 3", len(got))
	}
	eth0 := got[0]
	want := netdevStats{
		Iface:   "eth0",
		RxBytes: 4653697, RxPackets: 8110, RxErrs: 0, RxDrop: 0,
		TxBytes: 1847905, TxPackets: 7085, TxErrs: 0, TxDrop: 0,
	}
	if eth0 != want {
		t.Errorf("eth0 = %+v, want %+v", eth0, want)
	}
	// lo must be included per spec.
	if got[2].Iface != "lo" || got[2].RxBytes != 9388 || got[2].TxBytes != 9388 {
		t.Errorf("lo = %+v", got[2])
	}
}

func TestParseNetdevEmpty(t *testing.T) {
	headerOnly := `Inter-|   Receive ... |  Transmit
 face |bytes ...`
	if _, err := parseNetdev(headerOnly); err == nil {
		t.Fatal("expected error when no interface rows, got nil")
	}
}
