package config

import (
	"reflect"
)

// Reload reads the config again, as [Load] did: from the file this Config
// was read from, or — when Load found none — from wherever Load would find
// one now, so a file created since (by :theme, say) is read. The Config it
// is called on is left as it was; the caller decides what of the new one to
// use.
func (c *Config) Reload() (*Config, error) {
	path := c.path
	if path == "" {
		path = findConfigPath()
	}
	return loadFrom(path)
}

// ChangedSettings are the settings whose values differ between a and b,
// named by their TOML keys ("ui.parse_markdown") in the order Config
// declares its tables and their fields.
//
// It walks the struct rather than a list, so a setting added later is
// compared without anybody remembering to add it here. A setting is what
// [knownFields] says one is — the two share settingTables: a field with a
// toml tag, in a table with one.
// What Load keeps beside the settings — the file it read, the theme it
// resolved — has no tag and is not compared.
func ChangedSettings(a, b *Config) []string {
	return changedSettings(reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
}

// changedSettings is [ChangedSettings] for any two values of one config
// struct type.
func changedSettings(a, b reflect.Value) []string {
	var changed []string
	for _, t := range settingTables(a.Type()) {
		for _, f := range t.fields {
			if !sameSetting(a.Field(t.index).Field(f.index), b.Field(t.index).Field(f.index)) {
				changed = append(changed, t.name+"."+f.key)
			}
		}
	}
	return changed
}

// sameSetting reports whether two values of a setting say the same thing.
// A list or a map left out and one written empty do — `send_dirs = []`
// and no send_dirs decode to an empty slice and a nil one — so two empty
// ones are equal however they got that way.
func sameSetting(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Slice, reflect.Map:
		if a.Len() == 0 && b.Len() == 0 {
			return true
		}
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}
