package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"strings"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

const (
	UploadOriginal     = "original"
	UploadWebPLossless = "webp_lossless"
	UploadWebPQuality  = "webp_quality"

	ThumbnailJPEG     = "jpg"
	ThumbnailWebP     = "webp"
	ThumbnailDisabled = "disabled"

	DefaultWebPQuality      = 85
	DefaultThumbnailQuality = 82
	DefaultMemoryLimitMB    = 256
	maxWebPDimension        = 16_383
	estimatedBytesPerPixel  = 8
)

type UploadResult struct {
	Data      []byte
	Name      string
	Extension string
	MIME      string
	Width     int
	Height    int
	Processed bool
}

// ProcessUpload applies the configured storage policy to a raster image. SVG
// files are returned unchanged because rasterizing them would alter their
// semantics.
func ProcessUpload(data []byte, name, mode string, quality int, memoryLimitBytes int64) (UploadResult, error) {
	result := UploadResult{Data: data, Name: name}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	result.Extension = ext
	result.MIME = mimeForExtension(ext)

	if ext == "svg" || mode == "" || mode == UploadOriginal {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			result.Width = cfg.Width
			result.Height = cfg.Height
		}
		return result, nil
	}
	if mode != UploadWebPLossless && mode != UploadWebPQuality {
		return result, fmt.Errorf("unsupported image upload mode %q", mode)
	}

	if err := validateImageSize(data, maxWebPDimension, memoryLimitBytes); err != nil {
		return result, err
	}
	options := webp.Options{Lossless: mode == UploadWebPLossless, Quality: clampQuality(quality, DefaultWebPQuality)}
	var encoded bytes.Buffer
	width, height := 0, 0
	if ext == "gif" {
		animation, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return result, fmt.Errorf("decode gif: %w", err)
		}
		if len(animation.Image) > 1 {
			webpAnimation := gifToWebPAnimation(animation)
			if err := webp.EncodeAll(&encoded, webpAnimation, options); err != nil {
				return result, fmt.Errorf("encode animated webp: %w", err)
			}
			width = animation.Config.Width
			height = animation.Config.Height
		}
	} else if ext == "webp" {
		animation, err := webp.DecodeAll(bytes.NewReader(data))
		if err == nil && len(animation.Image) > 1 {
			if err := webp.EncodeAll(&encoded, animation, options); err != nil {
				return result, fmt.Errorf("re-encode animated webp: %w", err)
			}
			bounds := animation.Image[0].Bounds()
			width, height = bounds.Dx(), bounds.Dy()
		}
	}
	if encoded.Len() == 0 {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return result, fmt.Errorf("decode image: %w", err)
		}
		if err := webp.Encode(&encoded, img, options); err != nil {
			return result, fmt.Errorf("encode webp: %w", err)
		}
		bounds := img.Bounds()
		width, height = bounds.Dx(), bounds.Dy()
	}
	result.Data = encoded.Bytes()
	result.Name = replaceExtension(name, ".webp")
	result.Extension = "webp"
	result.MIME = "image/webp"
	result.Width = width
	result.Height = height
	result.Processed = true
	return result, nil
}

func Thumbnail(src io.Reader, format string, quality, maxWidth, maxHeight int, memoryLimitBytes int64) ([]byte, string, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, "", err
	}
	if err := validateImageSize(data, 0, memoryLimitBytes); err != nil {
		return nil, "", err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode thumbnail source: %w", err)
	}
	if maxWidth <= 0 || maxHeight <= 0 {
		return nil, "", fmt.Errorf("invalid thumbnail dimensions")
	}

	bounds := img.Bounds()
	if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight {
		width, height := fitDimensions(bounds.Dx(), bounds.Dy(), maxWidth, maxHeight)
		dst := image.NewNRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		img = dst
	}
	quality = clampQuality(quality, DefaultThumbnailQuality)

	var encoded bytes.Buffer
	switch NormalizeThumbnailFormat(format) {
	case ThumbnailWebP:
		if err := webp.Encode(&encoded, img, webp.Options{Quality: quality}); err != nil {
			return nil, "", fmt.Errorf("encode webp thumbnail: %w", err)
		}
		return encoded.Bytes(), "image/webp", nil
	default:
		if err := jpeg.Encode(&encoded, flattenAlpha(img), &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", fmt.Errorf("encode jpeg thumbnail: %w", err)
		}
		return encoded.Bytes(), "image/jpeg", nil
	}
}

func NormalizeThumbnailFormat(format string) string {
	if strings.EqualFold(strings.TrimSpace(format), ThumbnailWebP) {
		return ThumbnailWebP
	}
	return ThumbnailJPEG
}

func ClampQuality(quality, fallback int) int {
	return clampQuality(quality, fallback)
}

func replaceExtension(name, extension string) string {
	current := filepath.Ext(name)
	if current == "" {
		return name + extension
	}
	return strings.TrimSuffix(name, current) + extension
}

func mimeForExtension(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

func clampQuality(quality, fallback int) int {
	if fallback < 1 || fallback > 100 {
		fallback = DefaultThumbnailQuality
	}
	if quality < 1 || quality > 100 {
		return fallback
	}
	return quality
}

func flattenAlpha(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Over)
	return dst
}

func fitDimensions(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	if width*maxHeight > height*maxWidth {
		return maxWidth, max(1, height*maxWidth/width)
	}
	return max(1, width*maxHeight/height), maxHeight
}

func validateImageSize(data []byte, maxDimension int, memoryLimitBytes int64) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return validateImageDimensions(cfg.Width, cfg.Height, maxDimension, memoryLimitBytes)
}

func validateImageDimensions(width, height, maxDimension int, memoryLimitBytes int64) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions")
	}
	if maxDimension > 0 && (width > maxDimension || height > maxDimension) {
		return fmt.Errorf("image dimensions exceed the target format limit")
	}
	if memoryLimitBytes <= 0 {
		memoryLimitBytes = int64(DefaultMemoryLimitMB) << 20
	}
	estimatedBytes := int64(width) * int64(height) * estimatedBytesPerPixel
	if estimatedBytes > memoryLimitBytes {
		return fmt.Errorf("estimated image processing memory %d MB exceeds the %d MB limit", (estimatedBytes+(1<<20)-1)>>20, memoryLimitBytes>>20)
	}
	return nil
}

func gifToWebPAnimation(src *gif.GIF) *webp.WEBP {
	bounds := image.Rect(0, 0, src.Config.Width, src.Config.Height)
	canvas := image.NewNRGBA(bounds)
	frames := make([]image.Image, 0, len(src.Image))
	delays := make([]int, 0, len(src.Image))
	for i, frame := range src.Image {
		previous := cloneNRGBA(canvas)
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		frames = append(frames, cloneNRGBA(canvas))
		delay := 0
		if i < len(src.Delay) {
			delay = src.Delay[i] * 10
		}
		delays = append(delays, delay)

		disposal := byte(gif.DisposalNone)
		if i < len(src.Disposal) {
			disposal = src.Disposal[i]
		}
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = previous
		}
	}
	return &webp.WEBP{Image: frames, Delay: delays, LoopCount: src.LoopCount}
}

func cloneNRGBA(src image.Image) *image.NRGBA {
	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}
