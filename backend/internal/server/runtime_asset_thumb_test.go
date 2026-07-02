package server

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func TestResolveThumbWidth(t *testing.T) {
	cases := map[string]int{
		"":     0,
		"abc":  0,
		"100":  0,
		"256":  256,
		"512":  512,
		"1024": 0,
	}
	for in, want := range cases {
		if got := resolveThumbWidth(in); got != want {
			t.Errorf("resolveThumbWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestGenerateThumbnail_DownscalesAndCaches(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writePNG(t, src, 1024, 1024)

	dst := thumbCachePath(src, 512)
	data, err := generateThumbnail(src, dst, 512)
	if err != nil {
		t.Fatalf("generateThumbnail: %v", err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}
	if got := img.Bounds().Dx(); got != 512 {
		t.Errorf("thumb width = %d, want 512", got)
	}
	if got := img.Bounds().Dy(); got != 512 {
		t.Errorf("thumb height = %d, want 512", got)
	}
}

func TestGenerateThumbnail_SkipsWhenSourceSmaller(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "small.png")
	writePNG(t, src, 200, 200)

	dst := thumbCachePath(src, 512)
	_, err := generateThumbnail(src, dst, 512)
	if err != errSkipThumb {
		t.Errorf("expected errSkipThumb, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("cache file should not exist when skip signaled")
	}
}

func TestGenerateThumbnailRejectsOversizedConfig(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "oversized.jpg")

	_, err := generateThumbnailFromBytes(pngConfigOnly(maxThumbSourceDimension+1, 1), dst, 256)
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("oversized config error = %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("oversized source should not write cache: %v", err)
	}
}

func TestThumbFailureCacheMarker(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.png")
	markThumbFailure(src, 256)
	if !thumbFailureCached(src, 256) {
		t.Fatal("fresh thumb failure marker should be cached")
	}

	marker := thumbFailureCachePath(src, 256)
	old := time.Now().Add(-thumbFailureCacheTTL - time.Minute)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatalf("age marker: %v", err)
	}
	if thumbFailureCached(src, 256) {
		t.Fatal("stale thumb failure marker should not be cached")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale marker should be removed: %v", err)
	}
}

func TestThumbnailableExt(t *testing.T) {
	cases := map[string]bool{
		"foo.png":  true,
		"foo.PNG":  true,
		"foo.jpg":  true,
		"foo.jpeg": true,
		"foo.webp": true,
		"foo.gif":  true,
		"foo.svg":  false,
		"foo.txt":  false,
		"foo":      false,
	}
	for in, want := range cases {
		if got := thumbnailableExt(in); got != want {
			t.Errorf("thumbnailableExt(%q) = %v, want %v", in, got, want)
		}
	}
}

func pngConfigOnly(width, height int) []byte {
	var out bytes.Buffer
	out.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8
	ihdr[9] = 2
	writePNGChunk(&out, "IHDR", ihdr)
	writePNGChunk(&out, "IEND", nil)
	return out.Bytes()
}

func writePNGChunk(out *bytes.Buffer, kind string, data []byte) {
	_ = binary.Write(out, binary.BigEndian, uint32(len(data)))
	out.WriteString(kind)
	out.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(kind))
	_, _ = crc.Write(data)
	_ = binary.Write(out, binary.BigEndian, crc.Sum32())
}
