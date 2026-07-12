package collector

import "testing"

// dfSample is the real toybox `df /data /cache` output from the reference
// device (SPEC.md §6.7). Note /data resolves to /data/user/0.
const dfSample = `Filesystem            1K-blocks    Used Available Use% Mounted on
/dev/block/mmcblk0p32  12203000 3397576   8657968  29% /data/user/0
/dev/block/mmcblk0p19     61360   10716     48624  19% /cache`

func TestParseDF(t *testing.T) {
	got, err := parseDF(dfSample)
	if err != nil {
		t.Fatalf("parseDF returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	data := got[0]
	if data.Mountpoint != "/data/user/0" {
		t.Errorf("mountpoint = %q, want /data/user/0", data.Mountpoint)
	}
	if data.SizeBytes != 12203000*1024 || data.UsedBytes != 3397576*1024 || data.AvailBytes != 8657968*1024 {
		t.Errorf("data bytes = %+v", data)
	}
	cache := got[1]
	if cache.Mountpoint != "/cache" || cache.SizeBytes != 61360*1024 {
		t.Errorf("cache = %+v", cache)
	}
}

func TestParseDFEmpty(t *testing.T) {
	if _, err := parseDF("Filesystem 1K-blocks Used Available Use% Mounted on"); err == nil {
		t.Fatal("expected error when only header present, got nil")
	}
}
