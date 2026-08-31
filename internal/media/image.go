package media

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
)

const (
	maxImageFileBytes = 20 << 20
	maxImagePixels    = 20_000_000
)

type ImageRenderer struct {
	protocol  Protocol
	maxWidth  int
	maxHeight int
}

func NewImageRenderer(protocol Protocol, maxWidth, maxHeight int) *ImageRenderer {
	return &ImageRenderer{
		protocol:  protocol,
		maxWidth:  maxWidth,
		maxHeight: maxHeight,
	}
}

func (r *ImageRenderer) RenderFile(path string) (string, error) {
	img, err := r.decodeFile(path)
	if err != nil {
		return "", err
	}
	return r.RenderImage(img)
}

// decodeFile is RenderFile's front half: the size and bounds guards, then the
// decode. Split out so PlaceFile cannot acquire a copy of the guards that
// drifts from this one — the whole point of them is that no path into the
// decoder skips them.
func (r *ImageRenderer) decodeFile(path string) (image.Image, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("opening image: %w", err)
	}
	if err := checkImageFileSize(info.Size()); err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening image: %w", err)
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	if err := checkImageBounds(info.Size(), cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	return img, nil
}

func checkImageFileSize(fileSize int64) error {
	if fileSize > maxImageFileBytes {
		return fmt.Errorf("image too large")
	}
	return nil
}

func checkImageBounds(fileSize int64, width, height int) error {
	if err := checkImageFileSize(fileSize); err != nil {
		return err
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("image dimensions too large")
	}
	if height > maxImagePixels/width {
		return fmt.Errorf("image dimensions too large")
	}
	return nil
}

func (r *ImageRenderer) RenderImage(img image.Image) (string, error) {
	p, err := r.PlaceImage(img)
	return p.Art, err
}

// Placement is a rendered image together with whatever it takes to remove it
// again.
//
// The two protocols differ in who owns the pixels. Sixel and half-blocks are
// written into the cell grid, so redrawing those cells erases them and
// Teardown is empty. A kitty image belongs to the TERMINAL: it survives any
// number of text redraws, and a caller that draws one over the UI and then
// stops drawing it has left it on the screen. Teardown is that caller's way
// to say it is finished with the image.
type Placement struct {
	Art      string
	Teardown string
}

// PlaceFile renders a file and reports how to remove it.
func (r *ImageRenderer) PlaceFile(path string) (Placement, error) {
	img, err := r.decodeFile(path)
	if err != nil {
		return Placement{}, err
	}
	return r.PlaceImage(img)
}

// PlaceImage renders an already-decoded image and reports how to remove it.
func (r *ImageRenderer) PlaceImage(img image.Image) (Placement, error) {
	img = resizeToFit(img, r.maxWidth, r.maxHeight)

	switch r.protocol {
	case ProtocolKitty:
		art, id, err := renderKittyWithID(img)
		if err != nil {
			return Placement{}, err
		}
		return Placement{Art: art, Teardown: KittyDelete(id)}, nil
	case ProtocolSixel:
		art, err := renderSixel(img)
		return Placement{Art: art}, err
	default:
		return Placement{Art: renderBlocks(img)}, nil
	}
}

// Protocol reports which protocol this renderer draws with.
func (r *ImageRenderer) Protocol() Protocol { return r.protocol }

// resizeToFit scales image to fit terminal dimensions.
// For blocks: each column = 1 pixel wide, each row = 2 pixels tall (half-blocks).
func resizeToFit(img image.Image, maxCols, maxRows int) image.Image {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Target pixel dimensions
	targetW := maxCols
	targetH := maxRows * 2 // 2 pixels per row with half-blocks

	if srcW <= targetW && srcH <= targetH {
		return img
	}

	scaleW := float64(targetW) / float64(srcW)
	scaleH := float64(targetH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	// Use CatmullRom for high-quality scaling
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	return dst
}
