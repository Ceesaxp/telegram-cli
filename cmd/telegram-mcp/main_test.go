package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const helpTestProcessEnv = "TELEGRAM_MCP_HELP_TEST_PROCESS"

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

func TestMainHelpExitsSuccessfully(t *testing.T) {
	if args, ok := os.LookupEnv(helpTestProcessEnv); ok {
		os.Args = append([]string{"telegram-mcp"}, strings.Fields(args)...)
		main()
		return
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "long flag", args: []string{"--help"}},
		{name: "short flag", args: []string{"-h"}},
		{name: "login help", args: []string{"login", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelpExitsSuccessfully$")
			cmd.Env = append(os.Environ(), helpTestProcessEnv+"="+strings.Join(tt.args, " "))

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				t.Fatalf("telegram-mcp %s failed: %v\nstderr:\n%s", strings.Join(tt.args, " "), err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "usage: telegram-mcp [login [--qr]|serve]") {
				t.Fatalf("expected usage on stdout, got %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}
