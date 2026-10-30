// Package media membaca metadata Matroska yang dipakai player: judul, tag,
// chapter, daftar track, dan cover art. Isinya hanya media yang sudah ada di
// file sumber; tidak ada transcode atau perubahan stream.
package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
)

// Info is the player-facing subset of an MKV's metadata.
type Info struct {
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Rating      string    `json:"rating,omitempty"`
	Genre       string    `json:"genre,omitempty"`
	Date        string    `json:"date,omitempty"`
	Duration    float64   `json:"duration,omitempty"` // detik, dari Info container (head-only)
	Chapters    []Chapter `json:"chapters"`
	Video       []Track   `json:"video"`
	Audio       []Track   `json:"audio"`
	Subs        []Track   `json:"subs"`
	Cover       []byte    `json:"-"`
	CoverMIME   string    `json:"-"`
}

// Chapter is one Matroska chapter, converted to the seconds MoviPlayer wants.
type Chapter struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end,omitempty"`
}

// Track is the technical/language information of one source track.
type Track struct {
	Codec      string  `json:"codec,omitempty"`
	Lang       string  `json:"lang,omitempty"`
	Name       string  `json:"name,omitempty"`
	Width      uint32  `json:"width,omitempty"`
	Height     uint32  `json:"height,omitempty"`
	FPS        float64 `json:"fps,omitempty"`
	Channels   uint8   `json:"channels,omitempty"`
	SampleRate float64 `json:"sample_rate,omitempty"`
	BitDepth   uint16  `json:"bit_depth,omitempty"`
	Default    bool    `json:"default,omitempty"`
	Forced     bool    `json:"forced,omitempty"`
}

const maxCoverBytes = 8 << 20

// Read parses the head metadata of a Matroska file. It asks mkvgo for chapters,
// tags and attachment headers, but never loads font payloads; only a small
// cover image is read into memory.
func Read(ctx context.Context, rs io.ReadSeeker, name string) (Info, error) {
	c, err := matroska.ReadMeta(ctx, rs, name,
		matroska.WithChapters(),
		matroska.WithTags(),
		matroska.WithAttachments(),
		matroska.WithoutAttachmentData(),
	)
	if err != nil {
		return Info{}, err
	}

	info := Info{
		Title:       firstNonEmpty(c.Info.Title, tagValue(c.Tags, "TITLE")),
		Description: tagValue(c.Tags, "DESCRIPTION", "SYNOPSIS", "SUMMARY", "COMMENT"),
		Rating:      tagValue(c.Tags, "RATING", "RATING_VALUE", "AGE_RATING"),
		Genre:       tagValue(c.Tags, "GENRE"),
		Date:        tagValue(c.Tags, "DATE_RELEASED", "DATE", "YEAR"),
		Duration:    float64(c.DurationMs) / 1000,
		Chapters:    make([]Chapter, 0, len(c.Chapters)),
		Video:       make([]Track, 0, 1),
		Audio:       make([]Track, 0, 2),
		Subs:        make([]Track, 0, 2),
	}
	for _, ch := range c.Chapters {
		info.Chapters = append(info.Chapters, Chapter{
			Title: firstNonEmpty(ch.Title, fmt.Sprintf("Chapter %d", len(info.Chapters)+1)),
			Start: float64(ch.StartMs) / 1000,
			End:   float64(ch.EndMs) / 1000,
		})
	}
	sort.SliceStable(info.Chapters, func(i, j int) bool {
		return info.Chapters[i].Start < info.Chapters[j].Start
	})

	for _, t := range c.Tracks {
		track := Track{
			Codec:    t.Codec,
			Lang:     t.ResolvedLanguage(),
			Name:     t.Name,
			Default:  t.IsDefault,
			Forced:   t.IsForced,
			Channels: deref(t.Channels),
			BitDepth: deref(t.VideoBitDepth),
		}
		if t.SampleRate != nil {
			track.SampleRate = *t.SampleRate
		}
		if t.Width != nil {
			track.Width = *t.Width
		}
		if t.Height != nil {
			track.Height = *t.Height
		}
		if t.FrameRate != nil {
			track.FPS = *t.FrameRate
		}
		switch t.Type {
		case mkv.VideoTrack:
			info.Video = append(info.Video, track)
		case mkv.AudioTrack:
			info.Audio = append(info.Audio, track)
		case mkv.SubtitleTrack:
			info.Subs = append(info.Subs, track)
		}
	}

	if att, ok := coverAttachment(c.Attachments); ok {
		cover, err := readAttachment(rs, att)
		if err == nil && len(cover) > 0 {
			info.Cover = cover
			info.CoverMIME = att.MIMEType
			if info.CoverMIME == "" {
				info.CoverMIME = http.DetectContentType(cover)
			}
		}
	}
	return info, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func tagValue(tags []mkv.Tag, names ...string) string {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[strings.ToUpper(n)] = true
	}
	var fallback string
	for _, tag := range tags {
		for _, st := range tag.SimpleTags {
			if !want[strings.ToUpper(st.Name)] || st.Value == "" {
				continue
			}
			// Target type 50 adalah level film; kosong juga sering dipakai.
			if tag.TargetType == "50" || tag.TargetType == "" {
				return st.Value
			}
			if fallback == "" {
				fallback = st.Value
			}
		}
	}
	return fallback
}

func coverAttachment(atts []mkv.Attachment) (mkv.Attachment, bool) {
	var best mkv.Attachment
	found := false
	for _, att := range atts {
		if !strings.HasPrefix(strings.ToLower(att.MIMEType), "image/") || att.Size <= 0 || att.Size > maxCoverBytes {
			continue
		}
		name := strings.ToLower(att.Name)
		isCover := strings.Contains(name, "cover") || strings.Contains(name, "poster")
		if !found || isCover {
			best, found = att, true
		}
		if isCover {
			break
		}
	}
	return best, found
}

func readAttachment(rs io.ReadSeeker, att mkv.Attachment) ([]byte, error) {
	if len(att.Data) > 0 {
		return att.Data, nil
	}
	if att.DataOffset <= 0 {
		return nil, fmt.Errorf("attachment %q tanpa payload", att.Name)
	}
	if _, err := rs.Seek(att.DataOffset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, att.Size)
	_, err := io.ReadFull(rs, buf)
	return buf, err
}

func deref[T any](p *T) (v T) {
	if p != nil {
		return *p
	}
	return v
}
