package vision

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

// Image represents an image for vision processing.
type Image struct {
	Data     []byte
	MIMEType string
	FileName string
}

// LoadImage loads an image from a file path.
func LoadImage(path string) (*Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}

	mime := detectMIME(data)

	return &Image{
		Data:     data,
		MIMEType: mime,
		FileName: path,
	}, nil
}

// FromBase64 creates an image from base64-encoded data.
func FromBase64(base64Data string, mime string) *Image {
	decoded, _ := base64.StdEncoding.DecodeString(base64Data)
	return &Image{
		Data:     decoded,
		MIMEType: mime,
	}
}

// ToBase64 converts image to base64 string.
func (i *Image) ToBase64() string {
	return base64.StdEncoding.EncodeToString(i.Data)
}

// ToURL creates a data URL for the image.
func (i *Image) ToURL() string {
	return fmt.Sprintf("data:%s;base64,%s", i.MIMEType, i.ToBase64())
}

// Size returns the image size in bytes.
func (i *Image) Size() int {
	return len(i.Data)
}

// detectMIME detects MIME type from file content.
func detectMIME(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// Check PNG signature
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	// Check JPEG signature
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// Check GIF signature
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}

	// Check WebP signature (RIFF....WEBP)
	if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
		return "image/webp"
	}

	return "application/octet-stream"
}
