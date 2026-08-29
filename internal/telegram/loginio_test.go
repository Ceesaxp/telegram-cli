package telegram

import (
	"os"
	"testing"
)

func TestReadAuthLineVisible(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	go func() {
		_, _ = w.WriteString("hello\n")
		_ = w.Close()
	}()

	got, err := ReadAuthLine(r, false)
	if err != nil {
		t.Fatalf("ReadAuthLine: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestReadAuthLineHiddenOnPipeFallsBack(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	go func() {
		_, _ = w.WriteString("secret\n")
		_ = w.Close()
	}()

	got, err := ReadAuthLine(r, true)
	if err != nil {
		t.Fatalf("ReadAuthLine: %v", err)
	}
	if got != "secret" {
		t.Fatalf("got %q, want %q", got, "secret")
	}
}
