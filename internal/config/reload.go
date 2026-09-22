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
// [knownFields] says one is: a field with a toml tag, in a table with one.
// What Load keeps beside the settings — the file it read, the theme it
// resolved — has no tag and is not compared.
func ChangedSettings(a, b *Config) []string {
	var changed []string
	va, vb := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	cfgType := va.Type()
	for i := range cfgType.NumField() {
		section := cfgType.Field(i)
		table := section.Tag.Get("toml")
		if table == "" || section.Type.Kind() != reflect.Struct {
			continue
		}
		for j := range section.Type.NumField() {
			tag := section.Type.Field(j).Tag.Get("toml")
			if tag == "" {
				continue
			}
			if !reflect.DeepEqual(va.Field(i).Field(j).Interface(), vb.Field(i).Field(j).Interface()) {
				changed = append(changed, table+"."+tag)
			}
		}
	}
	return changed
}
