package hls

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
	"github.com/gravity-zero/mkvgo/mkv/writer"
)

// fixture writes a Matroska file with one H.264 track, one audio track per
// language and clusters cut on keyframes every second.
func fixture(t *testing.T, langs ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.mkv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("buat fixture: %v", err)
	}
	defer f.Close()

	c := &matroska.Container{Info: mkv.SegmentInfo{TimecodeScale: 1_000_000, MuxingApp: "test", WritingApp: "test"}}
	w, h := uint32(320), uint32(240)
	sr := 48000.0
	ch := uint8(2)
	tracks := []mkv.Track{{ID: 1, Type: mkv.VideoTrack, Codec: "h264", Width: &w, Height: &h, CodecPrivate: []byte{1}}}
	for i, lang := range langs {
		tracks = append(tracks, mkv.Track{ID: uint64(i + 2), Type: mkv.AudioTrack, Codec: "aac",
			SampleRate: &sr, Channels: &ch, Language: lang,
			CodecPrivate: []byte{0x11, 0x90}}) // AAC-LC 48 kHz stereo
	}

	mw := writer.NewMKVWriter(f)
	if err := mw.WriteStart(); err != nil {
		t.Fatalf("mulai fixture: %v", err)
	}
	// Durasi 4 dtk di Info (bukan 1): writer lama menulisnya salah.
	if err := mw.WriteMetadata(c, tracks, 4000); err != nil {
		t.Fatalf("metadata fixture: %v", err)
	}
	for tc := int64(0); tc < 4000; tc += 500 {
		var blocks []mkv.Block
		if tc%1000 == 0 {
			blocks = append(blocks, mkv.Block{TrackNumber: 1, Timecode: tc, Keyframe: true, Data: []byte("videoframe")})
		}
		for i := range langs {
			blocks = append(blocks, mkv.Block{TrackNumber: uint64(i + 2), Timecode: tc, Keyframe: true, Data: []byte("a")})
		}
		if err := mw.WriteClusterWithCues(tc, 1_000_000, blocks); err != nil {
			t.Fatalf("cluster fixture: %v", err)
		}
	}
	if err := mw.Finalize(); err != nil {
		t.Fatalf("finalisasi fixture: %v", err)
	}
	return path
}

func waitReady(t *testing.T, m *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, exists := m.Status(); exists && (st.Ready || st.Error != "") {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("playlist hls tidak muncul dalam batas waktu")
	return Status{}
}

func TestSessionPublishesPlaylist(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 2, fixture(t, "jpn", "eng"))

	st := waitReady(t, m)
	if st.Error != "" {
		t.Fatalf("sesi gagal: %s", st.Error)
	}
	if st.Index != 2 || st.Playlist != "master.m3u8" || !st.Ready {
		t.Fatalf("Status = %+v", st)
	}

	ctx := context.Background()
	master, ok := m.Playlist(ctx, 2, "master.m3u8")
	if !ok {
		t.Fatal("master.m3u8 tidak tersedia")
	}
	if got := countLines(string(master), "#EXT-X-MEDIA:TYPE=AUDIO"); got != 2 {
		t.Fatalf("jumlah rendition audio = %d, want 2:\n%s", got, master)
	}
	if _, ok := m.Playlist(ctx, 3, "master.m3u8"); ok {
		t.Fatal("index lain tidak boleh dilayani")
	}
}

func TestSegmentAndSubtitle(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 0, fixture(t, "jpn"))
	if st := waitReady(t, m); st.Error != "" {
		t.Fatalf("sesi gagal: %s", st.Error)
	}

	ctx := context.Background()
	seg, contentType, err := m.Segment(ctx, 0, "seg00001.m4s")
	if err != nil || len(seg) == 0 {
		t.Fatalf("Segment: %v (%d byte)", err, len(seg))
	}
	if contentType != "video/iso.segment" {
		t.Fatalf("content-type segmen = %q", contentType)
	}
	if _, _, err := m.Segment(ctx, 0, "../../etc/passwd"); err == nil {
		t.Fatal("nama di luar sesi harus error")
	}
	if _, err := m.Subtitle(ctx, 0, "subs0.vtt"); err == nil {
		t.Fatal("fixture tanpa subtitle tidak boleh punya subs0.vtt")
	}
}

func TestUnknownIndexIsNotFound(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 0, fixture(t))
	waitReady(t, m)

	if _, ok := m.Playlist(context.Background(), 5, "master.m3u8"); ok {
		t.Fatal("index 5 harus not found")
	}
	if _, _, err := m.Segment(context.Background(), 5, "seg00001.m4s"); err == nil {
		t.Fatal("index 5 harus ErrNoSuchFile")
	}
}

func countLines(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
