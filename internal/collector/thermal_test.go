package collector

import "testing"

// thermalSample mimics the relevant dumpsys thermalservice blocks from the
// reference device (SPEC.md §6.5), including a trailing non-Temperature line to
// exercise block termination and a Cached block that must be ignored when HAL
// is present.
const thermalSample = `IsStatusOverride: false
Current temperatures from HAL:
	Temperature{mValue=62.500004, mType=0, mName=CPU0, mStatus=0}
	Temperature{mValue=62.500004, mType=0, mName=CPU1, mStatus=0}
	Temperature{mValue=61.000004, mType=1, mName=GPU, mStatus=0}
Current cooling devices from HAL:
	CoolingDevice{mValue=0, mType=0, mName=thermal-cpufreq-0}
Cached temperatures:
	Temperature{mValue=99.9, mType=3, mName=SKIN, mStatus=0}`

func TestParseThermalHAL(t *testing.T) {
	got, err := parseThermal(thermalSample)
	if err != nil {
		t.Fatalf("parseThermal returned error: %v", err)
	}
	// Only the HAL block (3 readings), Cached ignored.
	want := []tempReading{
		{Value: 62.500004, Type: 0, Name: "CPU0"},
		{Value: 62.500004, Type: 0, Name: "CPU1"},
		{Value: 61.000004, Type: 1, Name: "GPU"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d readings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reading %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseThermalCachedFallback(t *testing.T) {
	cachedOnly := `Cached temperatures:
	Temperature{mValue=45.0, mType=2, mName=BATTERY, mStatus=0}`
	got, err := parseThermal(cachedOnly)
	if err != nil {
		t.Fatalf("parseThermal returned error: %v", err)
	}
	if len(got) != 1 || got[0] != (tempReading{Value: 45.0, Type: 2, Name: "BATTERY"}) {
		t.Errorf("got %+v", got)
	}
}

func TestParseThermalNone(t *testing.T) {
	if _, err := parseThermal("no temperatures here"); err == nil {
		t.Fatal("expected error when no Temperature entries, got nil")
	}
}

func TestThermalTypeLabel(t *testing.T) {
	cases := map[int]string{0: "CPU", 1: "GPU", 2: "BATTERY", 9: "NPU", 42: "unknown"}
	for typ, want := range cases {
		if got := thermalTypeLabel(typ); got != want {
			t.Errorf("thermalTypeLabel(%d) = %q, want %q", typ, got, want)
		}
	}
}
