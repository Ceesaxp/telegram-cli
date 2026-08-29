package telegram

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSanitizeDownloadFileName(t *testing.T) {
	tests := map[string]string{
		"photo:5211047124492479647:y.jpg": "photo_5211047124492479647_y.jpg",
		`question<1>:"draft"?.pdf`:        "question_1___draft__.pdf",
		"trailing. ":                      "trailing",
	}
	for input, want := range tests {
		if got := sanitizeDownloadFileName(input); got != want {
			t.Errorf("sanitizeDownloadFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFileRegistryDoCoalesces(t *testing.T) {
	r := newFileRegistry()
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func() (any, error) {
		calls.Add(1)
		<-release
		return "ok", nil
	}

	const n = 2
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			v, err := r.do("k", fn)
			if err != nil {
				errc <- err
				return
			}
			if v != "ok" {
				errc <- fmt.Errorf("got %v", v)
				return
			}
			errc <- nil
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("do: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fn ran %d times, want 1", got)
	}
}

func TestDownloadFileSyncUnknownKey(t *testing.T) {
	c := &Client{files: newFileRegistry()}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.DownloadFileSync("missing")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	n := 0
	for err := range errs {
		n++
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
	}
	if n != 2 {
		t.Fatalf("got %d results, want 2", n)
	}
}
