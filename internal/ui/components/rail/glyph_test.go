package rail

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// TestAPictureSharedAsAFileIsStillAPicture, and gets the mark the media
// card gives one, so the same file is the same glyph wherever it is drawn.
func TestAPictureSharedAsAFileIsStillAPicture(t *testing.T) {
	doc := func(mime string) *telegram.Message {
		return &telegram.Message{Content: &telegram.MessageDocument{
			Document: &telegram.Document{FileName: "f", MimeType: mime},
		}}
	}

	cases := map[string]struct {
		msg  *telegram.Message
		want bool
	}{
		"a png sent as a file":     {doc("image/png"), true},
		"a photo":                  {&telegram.Message{Content: &telegram.MessagePhoto{}}, true},
		"a patch":                  {doc("text/x-patch"), false},
		"a document with no mime":  {doc(""), false},
		"a document with none set": {&telegram.Message{Content: &telegram.MessageDocument{}}, false},
		"something else entirely":  {&telegram.Message{Content: &telegram.MessageVoiceNote{}}, false},
	}
	for name, tc := range cases {
		if got := isImageFile(tc.msg); got != tc.want {
			t.Errorf("%s: isImageFile = %v, want %v", name, got, tc.want)
		}
	}
}

// TestTheImageRowHasItsOwnGlyph, and it is not the one every other file
// gets.
func TestTheImageRowHasItsOwnGlyph(t *testing.T) {
	m := Model{}
	image, _ := m.glyphFor(Row{Kind: RowFileImage})
	file, _ := m.glyphFor(Row{Kind: RowFile})

	if image == file {
		t.Fatalf("a picture and a patch are both %q", image)
	}
	if image != "▣" {
		t.Errorf("image glyph = %q, want the one the media card draws", image)
	}
}
