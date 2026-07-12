package collector

import "testing"

func TestParseForeground(t *testing.T) {
	// Real ResumedActivity line from the reference device (SPEC.md §6.9).
	in := "  ResumedActivity: ActivityRecord{903e9f8 u0 com.spocky.projengmenu/.ui.home.MainActivity t1554}"
	pkg, activity, found := parseForeground(in)
	if !found {
		t.Fatal("expected found=true")
	}
	if pkg != "com.spocky.projengmenu" {
		t.Errorf("package = %q, want com.spocky.projengmenu", pkg)
	}
	if activity != ".ui.home.MainActivity" {
		t.Errorf("activity = %q, want .ui.home.MainActivity", activity)
	}
}

func TestParseForegroundNone(t *testing.T) {
	// Empty output (grep matched nothing) ⇒ no foreground app, not an error.
	if _, _, found := parseForeground(""); found {
		t.Error("expected found=false on empty input")
	}
	if _, _, found := parseForeground("some unrelated dumpsys line"); found {
		t.Error("expected found=false when no ResumedActivity line")
	}
}
