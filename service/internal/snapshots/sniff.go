package snapshots

import (
	"bytes"
	"path/filepath"
	"strings"
)

// ContentType decides what an upload is, the way the Rails endpoint decided:
// Marcel's rules, as recorded in service/conformance/testdata/content_types.json
// and replayed against this code by sniff_test.go and the conformance suite.
//
// Three outcomes, in order:
//   - the bytes are recognised: that type, whatever the client declared;
//   - they are not, and the client declared something other than
//     application/octet-stream: the declared type is believed;
//   - neither: the filename's extension decides, and without one the upload is
//     application/octet-stream -- not an image, and refused.
//
// "Recognised" is Marcel's magic table as far as it matters here. Notably BMP's
// two-byte "BM" is too weak for it and falls through to the declared type, and
// an ISO box with the heix brand is recognised as something that is not an
// image. Both are pinned by the corpus; neither is a judgement of ours.
func ContentType(head []byte, declared, filename string) string {
	if t := magic(head); t != "" {
		return t
	}
	declared = strings.ToLower(strings.TrimSpace(strings.SplitN(declared, ";", 2)[0]))
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	if t := byExtension[strings.ToLower(filepath.Ext(filename))]; t != "" {
		return t
	}
	return "application/octet-stream"
}

// IsImage is ActiveStorage::Blob#image?.
func IsImage(contentType string) bool {
	return strings.HasPrefix(contentType, "image")
}

var byExtension = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".jpe": "image/jpeg",
	".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".bmp": "image/bmp", ".tif": "image/tiff", ".tiff": "image/tiff",
	".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif",
	".svg": "image/svg+xml",
	".txt": "text/plain", ".html": "text/html", ".pdf": "application/pdf", ".zip": "application/zip",
	".mp4": "video/mp4",
}

// ISO base media brands, at offset 8 of an `ftyp` box.
var ftypBrands = map[string]string{
	"heic": "image/heic",
	"heis": "image/heic-sequence",
	"hevc": "image/heic-sequence",
	"mif1": "image/heif",
	"msf1": "image/heif-sequence",
	"avif": "image/avif",
	"avis": "image/avif",
	// Recognised, and not as images. heix is pinned by the corpus.
	"heix": "video/mp4",
	"isom": "video/mp4",
	"iso2": "video/mp4",
	"mp41": "video/mp4",
	"mp42": "video/mp4",
	"avc1": "video/mp4",
	"M4V ": "video/x-m4v",
	"qt  ": "video/quicktime",
	"3gp4": "video/3gpp",
	"3gp5": "video/3gpp",
}

func magic(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(b, []byte("II*\x00")), bytes.HasPrefix(b, []byte("MM\x00*")):
		return "image/tiff"
	case len(b) >= 12 && bytes.Equal(b[4:8], []byte("ftyp")):
		if t, ok := ftypBrands[string(b[8:12])]; ok {
			return t
		}
		return ""
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return "application/pdf"
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return "application/zip"
	}
	text := strings.ToLower(string(b[:min(len(b), 512)]))
	trimmed := strings.TrimLeft(text, " \t\r\n\ufeff")
	switch {
	case strings.HasPrefix(trimmed, "<svg"), strings.HasPrefix(trimmed, "<?xml") && strings.Contains(text, "<svg"):
		return "image/svg+xml"
	case strings.HasPrefix(trimmed, "<!doctype html"), strings.HasPrefix(trimmed, "<html"):
		return "text/html"
	}
	return ""
}
