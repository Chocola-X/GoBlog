package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcessUploadConvertsToWebP(t *testing.T) {
	result, err := ProcessUpload(testPNG(t, 40, 24), "cover.png", UploadWebPQuality, 76, int64(DefaultMemoryLimitMB)<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Processed || result.Name != "cover.webp" || result.Extension != "webp" || result.MIME != "image/webp" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Width != 40 || result.Height != 24 || !bytes.HasPrefix(result.Data, []byte("RIFF")) {
		t.Fatalf("unexpected converted image metadata")
	}
}

func TestThumbnailFitsBoundsAndEncodesConfiguredFormat(t *testing.T) {
	data, mimeType, err := Thumbnail(bytes.NewReader(testPNG(t, 640, 320)), ThumbnailWebP, 80, 160, 100, int64(DefaultMemoryLimitMB)<<20)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/webp" || !bytes.HasPrefix(data, []byte("RIFF")) {
		t.Fatalf("thumbnail mime/data = %q %q", mimeType, data[:4])
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 160 || cfg.Height != 80 {
		t.Fatalf("thumbnail size = %dx%d, want 160x80", cfg.Width, cfg.Height)
	}
}

func TestProcessUploadPreservesAnimatedGIFFrames(t *testing.T) {
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	for i := range first.Pix {
		first.Pix[i] = 1
		second.Pix[i] = 2
	}
	var source bytes.Buffer
	if err := gif.EncodeAll(&source, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{5, 7}, LoopCount: 2, Config: image.Config{ColorModel: palette, Width: 2, Height: 2}}); err != nil {
		t.Fatal(err)
	}
	result, err := ProcessUpload(source.Bytes(), "animated.gif", UploadWebPQuality, 80, int64(DefaultMemoryLimitMB)<<20)
	if err != nil {
		t.Fatal(err)
	}
	animation, err := webp.DecodeAll(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 2 || animation.Delay[0] != 50 || animation.Delay[1] != 70 {
		t.Fatalf("animated WebP metadata = %#v", animation)
	}
}

func TestLargePhotoMemoryBudget(t *testing.T) {
	if err := validateImageDimensions(9600, 6396, maxWebPDimension, int64(DefaultMemoryLimitMB)<<20); err == nil {
		t.Fatal("9600x6396 photo should exceed the default 256 MB budget")
	}
	if err := validateImageDimensions(9600, 6396, maxWebPDimension, 512<<20); err != nil {
		t.Fatalf("9600x6396 photo should fit a 512 MB budget: %v", err)
	}
	if err := validateImageDimensions(maxWebPDimension+1, 100, maxWebPDimension, 1024<<20); err == nil {
		t.Fatal("image wider than the WebP format limit should be rejected")
	}
}
