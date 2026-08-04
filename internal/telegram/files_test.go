package telegram

import "testing"

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
