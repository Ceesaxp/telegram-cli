package config

import "testing"

// Auto is the default because it is the only value that needs to know
// nothing about the terminal: it asks the terminal where the allowlist says
// that is safe, and the system otherwise.
//
// Defaulting to "system" would ship the behaviour this setting exists to get
// away from — on macOS, a notification posted as Script Editor, under Script
// Editor's name, icon and notification permission.
func TestNotifyMethodDefaultsToAuto(t *testing.T) {
	if got := defaultConfig().Notifications.Method; got != NotifyMethodAuto {
		t.Errorf("the default notifications.method is %q, want %q",
			got, NotifyMethodAuto)
	}
}

// An existing config predates the field, and a setting nobody can find is a
// setting that does not exist. The migration writes the key and names it in
// the report.
func TestMigrateAddsNotifyMethod(t *testing.T) {
	cfg := &Config{}
	changes := changeMap(t, Migrate(cfg, nil))

	c, ok := changes["notifications.method"]
	if !ok {
		t.Fatal("notifications.method was not filled in")
	}
	if !c.Absent {
		t.Error("notifications.method was reported as replacing a value it never had")
	}
	if c.New != NotifyMethodAuto {
		t.Errorf("notifications.method -> %q, want %q", c.New, NotifyMethodAuto)
	}
	if cfg.Notifications.Method != NotifyMethodAuto {
		t.Errorf("the config was not updated: %q", cfg.Notifications.Method)
	}
}
