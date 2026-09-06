// Package profilephoto owns the bounded employee-photo ingestion path used by
// the demo seeder and reusable by a future authenticated upload endpoint.
package profilephoto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	ProxySize        = 160
	MaxUploadBytes   = 12 << 20
	MaxSourcePixels  = 30_000_000
	proxyJPEGQuality = 86
)

var ErrInvalidUpload = errors.New("profilephoto: invalid upload")

type Request struct {
	WorkerKey         string
	Content           []byte
	DeclaredMediaType string
	OriginalName      string
	ProxyName         string
}

type Result struct {
	WorkerKey         string
	OriginalRef       string
	ProxyRef          string
	OriginalDigest    string
	ProxyDigest       string
	OriginalMediaType string
	ProxyMediaType    string
	OriginalBytes     int64
	ProxyBytes        int64
	SourceWidth       int
	SourceHeight      int
	ProxyWidth        int
	ProxyHeight       int
}

type Sink interface {
	PutOriginal(context.Context, string, []byte) (string, error)
	PutProxy(context.Context, string, []byte) (string, error)
}

// Upload validates the declared and sniffed types, decodes within fixed size
// bounds, retains the byte-exact source, and emits a square display proxy.
func Upload(ctx context.Context, sink Sink, req Request) (Result, error) {
	if sink == nil {
		return Result{}, fmt.Errorf("%w: sink is required", ErrInvalidUpload)
	}
	if strings.TrimSpace(req.WorkerKey) == "" || !safeFilename(req.OriginalName) || !safeFilename(req.ProxyName) {
		return Result{}, fmt.Errorf("%w: worker key and safe output names are required", ErrInvalidUpload)
	}
	if len(req.Content) == 0 || len(req.Content) > MaxUploadBytes {
		return Result{}, fmt.Errorf("%w: content size %d is outside 1..%d bytes", ErrInvalidUpload, len(req.Content), MaxUploadBytes)
	}
	declared := canonicalMediaType(req.DeclaredMediaType)
	if declared != "image/png" && declared != "image/jpeg" {
		return Result{}, fmt.Errorf("%w: media type %q is not allowed", ErrInvalidUpload, req.DeclaredMediaType)
	}
	sniffed := canonicalMediaType(http.DetectContentType(req.Content))
	if sniffed != declared {
		return Result{}, fmt.Errorf("%w: declared %s but content sniffed as %s", ErrInvalidUpload, declared, sniffed)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(req.Content))
	if err != nil {
		return Result{}, fmt.Errorf("%w: decode image metadata: %v", ErrInvalidUpload, err)
	}
	if config.Width < ProxySize || config.Height < ProxySize || int64(config.Width)*int64(config.Height) > MaxSourcePixels {
		return Result{}, fmt.Errorf("%w: source dimensions %dx%d are outside the supported bounds", ErrInvalidUpload, config.Width, config.Height)
	}
	if format != "png" && format != "jpeg" {
		return Result{}, fmt.Errorf("%w: decoded format %q is not allowed", ErrInvalidUpload, format)
	}
	source, _, err := image.Decode(bytes.NewReader(req.Content))
	if err != nil {
		return Result{}, fmt.Errorf("%w: decode pixels: %v", ErrInvalidUpload, err)
	}
	proxyImage := centerCropResize(source, ProxySize)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, proxyImage, &jpeg.Options{Quality: proxyJPEGQuality}); err != nil {
		return Result{}, fmt.Errorf("profilephoto: encode proxy: %w", err)
	}

	originalRef, err := sink.PutOriginal(ctx, req.OriginalName, append([]byte(nil), req.Content...))
	if err != nil {
		return Result{}, fmt.Errorf("profilephoto: retain original: %w", err)
	}
	proxyBytes := encoded.Bytes()
	proxyRef, err := sink.PutProxy(ctx, req.ProxyName, proxyBytes)
	if err != nil {
		return Result{}, fmt.Errorf("profilephoto: store proxy: %w", err)
	}
	return Result{
		WorkerKey: req.WorkerKey, OriginalRef: originalRef, ProxyRef: proxyRef,
		OriginalDigest: digest(req.Content), ProxyDigest: digest(proxyBytes), OriginalMediaType: declared, ProxyMediaType: "image/jpeg",
		OriginalBytes: int64(len(req.Content)), ProxyBytes: int64(len(proxyBytes)), SourceWidth: config.Width, SourceHeight: config.Height,
		ProxyWidth: ProxySize, ProxyHeight: ProxySize,
	}, nil
}

func canonicalMediaType(value string) string {
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed)
}

func safeFilename(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && !strings.ContainsAny(value, `/\\`)
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func centerCropResize(source image.Image, size int) *image.NRGBA {
	bounds := source.Bounds()
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	destination := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		sourceY := float64(top) + (float64(y)+0.5)*float64(side)/float64(size) - 0.5
		for x := 0; x < size; x++ {
			sourceX := float64(left) + (float64(x)+0.5)*float64(side)/float64(size) - 0.5
			destination.SetNRGBA(x, y, bilinear(source, sourceX, sourceY))
		}
	}
	return destination
}

func bilinear(source image.Image, x, y float64) color.NRGBA {
	bounds := source.Bounds()
	x0, y0 := clamp(int(x), bounds.Min.X, bounds.Max.X-1), clamp(int(y), bounds.Min.Y, bounds.Max.Y-1)
	x1, y1 := clamp(x0+1, bounds.Min.X, bounds.Max.X-1), clamp(y0+1, bounds.Min.Y, bounds.Max.Y-1)
	fx, fy := x-float64(x0), y-float64(y0)
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	c00 := color.NRGBAModel.Convert(source.At(x0, y0)).(color.NRGBA)
	c10 := color.NRGBAModel.Convert(source.At(x1, y0)).(color.NRGBA)
	c01 := color.NRGBAModel.Convert(source.At(x0, y1)).(color.NRGBA)
	c11 := color.NRGBAModel.Convert(source.At(x1, y1)).(color.NRGBA)
	interpolate := func(a, b, c, d uint8) uint8 {
		topValue := float64(a)*(1-fx) + float64(b)*fx
		bottomValue := float64(c)*(1-fx) + float64(d)*fx
		return uint8(topValue*(1-fy) + bottomValue*fy + 0.5)
	}
	return color.NRGBA{R: interpolate(c00.R, c10.R, c01.R, c11.R), G: interpolate(c00.G, c10.G, c01.G, c11.G), B: interpolate(c00.B, c10.B, c01.B, c11.B), A: interpolate(c00.A, c10.A, c01.A, c11.A)}
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// ReadAllBounded is the upload-edge helper: it never allocates beyond the
// package's hard limit and rejects an extra byte instead of truncating it.
func ReadAllBounded(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("%w: reader is required", ErrInvalidUpload)
	}
	body, err := io.ReadAll(io.LimitReader(reader, MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("profilephoto: read upload: %w", err)
	}
	if len(body) > MaxUploadBytes {
		return nil, fmt.Errorf("%w: upload exceeds %d bytes", ErrInvalidUpload, MaxUploadBytes)
	}
	return body, nil
}
