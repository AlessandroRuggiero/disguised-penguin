package models

import (
	"fmt"
	"strconv"
	"strings"
)

// Settings are global key -> integer enum values stored in the settings table.
// Each one is described by a SettingDef so validation, listing and completion
// all work from the same table; adding a setting only needs a new entry here.

type SettingKey string

const SettingAliasMode SettingKey = "alias_mode"

type AliasMode int

const (
	AliasNone AliasMode = 0
	AliasSome AliasMode = 1
	AliasAll  AliasMode = 2
)

type SettingValue struct {
	Value       int
	Name        string
	Description string
}

type SettingDef struct {
	Key         SettingKey
	Description string
	Default     int
	Values      []SettingValue
}

var settingDefs = []SettingDef{
	{
		Key:         SettingAliasMode,
		Description: "Which installed CLIs can be run without the 'dp' prefix (loaded via 'dp install-completions')",
		Default:     int(AliasNone),
		Values: []SettingValue{
			{int(AliasNone), "none", "no aliases"},
			{int(AliasSome), "some", "only CLIs enabled with 'dp alias add'"},
			{int(AliasAll), "all", "every installed CLI"},
		},
	},
}

func SettingDefs() []SettingDef { return settingDefs }

func LookupSetting(key string) (SettingDef, bool) {
	for _, def := range settingDefs {
		if string(def.Key) == key {
			return def, true
		}
	}
	return SettingDef{}, false
}

// ValueName returns the name of a value, or the number itself if it is unknown.
func (d SettingDef) ValueName(value int) string {
	for _, v := range d.Values {
		if v.Value == value {
			return v.Name
		}
	}
	return strconv.Itoa(value)
}

// Parse accepts either a value's name ("all") or its number ("2").
func (d SettingDef) Parse(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, numErr := strconv.Atoi(s)
	for _, v := range d.Values {
		if strings.EqualFold(v.Name, s) || (numErr == nil && v.Value == n) {
			return v.Value, nil
		}
	}
	allowed := make([]string, len(d.Values))
	for i, v := range d.Values {
		allowed[i] = fmt.Sprintf("%s (%d)", v.Name, v.Value)
	}
	return 0, fmt.Errorf("invalid value %q for %s, expected one of: %s", s, d.Key, strings.Join(allowed, ", "))
}
