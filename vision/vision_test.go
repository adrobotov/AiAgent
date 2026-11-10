package vision

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// base64Encode is a small helper for tests.
func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func TestDetectMIME(t *testing.T) {
	cases := map[string]string{
		"\x89PNG\r\n":    "image/png",
		"\xFF\xD8\xFF00": "image/jpeg",
		"GIF89a":         "image/gif",
		"RIFF0000":       "image/webp", // starts with RIFF
		"\x00\x00\x01":   "application/octet-stream",
		"abc":            "application/octet-stream", // too short
		"":               "application/octet-stream",
	}
	for in, want := range cases {
		if got := detectMIME([]byte(in)); got != want {
			t.Errorf("detectMIME(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	path := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(path, png, 0644); err != nil {
		t.Fatal(err)
	}

	img, err := LoadImage(path)
	if err != nil {
		t.Fatalf("LoadImage: %v", err)
	}
	if img.MIMEType != "image/png" {
		t.Fatalf("mime = %q", img.MIMEType)
	}
	if img.FileName != path {
		t.Fatalf("filename = %q", img.FileName)
	}
	if img.Size() != len(png) {
		t.Fatalf("size = %d, want %d", img.Size(), len(png))
	}
}

func TestLoadImageMissing(t *testing.T) {
	if _, err := LoadImage(filepath.Join(t.TempDir(), "nope.png")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestBase64RoundTrip(t *testing.T) {
	data := []byte("binary image data")
	img := FromBase64(base64Encode(data), "image/png")

	if img.MIMEType != "image/png" {
		t.Fatalf("mime = %q", img.MIMEType)
	}
	if string(img.Data) != string(data) {
		t.Fatalf("round trip failed: %q", img.Data)
	}
	if got := img.ToBase64(); got != base64Encode(data) {
		t.Fatalf("ToBase64 = %q", got)
	}
}

func TestToURL(t *testing.T) {
	img := &Image{Data: []byte("hi"), MIMEType: "image/jpeg"}
	url := img.ToURL()
	if !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Fatalf("unexpected URL: %q", url)
	}
	if !strings.HasSuffix(url, base64Encode([]byte("hi"))) {
		t.Fatalf("URL must end with base64 payload: %q", url)
	}
}
