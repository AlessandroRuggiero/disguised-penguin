package models

import "testing"

func TestSettingParse(t *testing.T) {
	def, ok := LookupSetting(string(SettingAliasMode))
	if !ok {
		t.Fatalf("alias_mode not registered")
	}
	for in, want := range map[string]int{"none": 0, "some": 1, "ALL": 2, "0": 0, "2": 2, " some ": 1} {
		got, err := def.Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
		} else if got != want {
			t.Errorf("Parse(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "3", "-1", "everything"} {
		if _, err := def.Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error", in)
		}
	}
}

func TestSettingValueName(t *testing.T) {
	def, _ := LookupSetting(string(SettingAliasMode))
	if got := def.ValueName(2); got != "all" {
		t.Errorf("ValueName(2) = %q", got)
	}
	if got := def.ValueName(7); got != "7" {
		t.Errorf("ValueName(7) = %q", got)
	}
}

func TestLookupSettingUnknown(t *testing.T) {
	if _, ok := LookupSetting("nope"); ok {
		t.Errorf("expected unknown setting")
	}
}
