// Package stream serves torrent file data over HTTP with range/seek support.
package stream

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Serve lets http.ServeContent handle HEAD, Range (206), multi-range and 416
// on top of any io.ReadSeeker, such as a torrent file reader.
func Serve(c *gin.Context, name string, r io.ReadSeeker) {
	if ct := videoMIME(filepath.Ext(name)); ct != "" {
		c.Header("Content-Type", ct)
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, r)
}

func videoMIME(ext string) string {
	switch strings.ToLower(ext) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".ts", ".m2ts":
		return "video/mp2t"
	}
	return ""
}
