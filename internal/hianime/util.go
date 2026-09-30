package hianime

import (
	"encoding/base64"
	"regexp"
)

var ReTag = regexp.MustCompile(`<[^>]*>`)

// ReImgHost allow-lists the image hosts the scrapers can hand out. Anything
// else the client asks to proxy is refused, so /img is not an open relay.
var ReImgHost = regexp.MustCompile(`^https://(cdn\.anipixcdn\.co|[^/]*\.?hianime\.at)/`)

// Base64RawURL encodes b with unpadded base64url.
func Base64RawURL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
