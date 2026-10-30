package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravity-zero/mkvgo/mkv"
	"github.com/gravity-zero/mkvgo/mkv/writer"
)

func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meta.mkv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	w, h := uint32(1920), uint32(1080)
	fps := 23.976
	sr := 48000.0
	ch := uint8(2)
	cover := []byte("\x89PNG\r\n\x1a\nfake-cover")
	c := &mkv.Container{
		Info: mkv.SegmentInfo{TimecodeScale: 1_000_000, Title: "Judul Asli", MuxingApp: "test", WritingApp: "test"},
		Chapters: []mkv.Chapter{
			{ID: 1, StartMs: 3000, EndMs: 6000, Title: "Kedua"},
			{ID: 2, StartMs: 0, EndMs: 3000, Title: "Pembuka"},
		},
		Tags: []mkv.Tag{{
			TargetType: "50",
			SimpleTags: []mkv.SimpleTag{
				{Name: "DESCRIPTION", Value: "Sinopsis singkat."},
				{Name: "RATING", Value: "8.5"},
				{Name: "GENRE", Value: "Anime"},
				{Name: "DATE_RELEASED", Value: "2024"},
			},
		}},
		Attachments: []mkv.Attachment{
			{ID: 1, Name: "font.ttf", MIMEType: "font/ttf", Data: []byte("font")},
			{ID: 2, Name: "cover.png", MIMEType: "image/png", Data: cover},
		},
	}
	tracks := []mkv.Track{
		{ID: 1, Type: mkv.VideoTrack, Codec: "h264", Width: &w, Height: &h, FrameRate: &fps, IsDefault: true, CodecPrivate: []byte{1}},
		{ID: 2, Type: mkv.AudioTrack, Codec: "aac", Language: "jpn", Name: "Japanese", SampleRate: &sr, Channels: &ch, IsDefault: true, CodecPrivate: []byte{0x11, 0x90}},
		{ID: 3, Type: mkv.SubtitleTrack, Codec: "srt", Language: "ind", Name: "Indonesian"},
	}

	mw := writer.NewMKVWriter(f)
	if err := mw.WriteStart(); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteMetadata(c, tracks, 6000); err != nil {
		t.Fatal(err)
	}
	for tc := int64(0); tc < 6000; tc += 1000 {
		if err := mw.WriteClusterWithCues(tc, 1_000_000, []mkv.Block{
			{TrackNumber: 1, Timecode: tc, Keyframe: true, Data: []byte("video")},
			{TrackNumber: 2, Timecode: tc, Keyframe: true, Data: []byte("audio")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Finalize(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadMetadata(t *testing.T) {
	path := fixture(t)
	rs, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()

	info, err := Read(context.Background(), rs, path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if info.Title != "Judul Asli" || info.Description != "Sinopsis singkat." || info.Rating != "8.5" || info.Genre != "Anime" || info.Date != "2024" {
		t.Fatalf("metadata = %+v", info)
	}
	if len(info.Chapters) != 2 || info.Chapters[0].Title != "Pembuka" || info.Chapters[0].Start != 0 ||
		info.Chapters[1].Title != "Kedua" || info.Chapters[1].End != 6 {
		t.Fatalf("chapter = %+v", info.Chapters)
	}
	if len(info.Video) != 1 || info.Video[0].Width != 1920 || info.Video[0].Height != 1080 || info.Video[0].FPS < 23.9 || info.Video[0].FPS > 24.1 {
		t.Fatalf("video = %+v", info.Video)
	}
	if len(info.Audio) != 1 || info.Audio[0].Lang != "jpn" || info.Audio[0].Channels != 2 || info.Audio[0].SampleRate != 48000 {
		t.Fatalf("audio = %+v", info.Audio)
	}
	if len(info.Subs) != 1 || info.Subs[0].Lang != "ind" || info.Subs[0].Name != "Indonesian" {
		t.Fatalf("subs = %+v", info.Subs)
	}
	if string(info.Cover) != "\x89PNG\r\n\x1a\nfake-cover" || info.CoverMIME != "image/png" {
		t.Fatalf("cover = %q (%s)", info.Cover, info.CoverMIME)
	}
}

func TestReadRejectsNonMatroska(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.mp4")
	if err := os.WriteFile(path, []byte("not matroska"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := mustOpen(t, path)
	defer func() { _ = rs.Close() }()
	if _, err := Read(context.Background(), rs, path); err == nil {
		t.Fatal("file non-Matroska harus error")
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
