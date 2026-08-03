package main

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		command string
		qr      bool
		wantErr bool
	}{
		{name: "default serve", command: "serve"},
		{name: "phone login", args: []string{"login"}, command: "login"},
		{name: "QR login", args: []string{"login", "--qr"}, command: "login", qr: true},
		{name: "serve rejects QR flag", args: []string{"serve", "--qr"}, wantErr: true},
		{name: "default serve rejects QR flag", args: []string{"--qr"}, wantErr: true},
		{name: "unknown command", args: []string{"unknown"}, wantErr: true},
		{name: "unexpected argument", args: []string{"login", "extra"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse command: %v", err)
			}
			if got.command != tt.command || got.qr != tt.qr {
				t.Fatalf("expected command=%q qr=%t, got command=%q qr=%t", tt.command, tt.qr, got.command, got.qr)
			}
		})
	}
}
