package app

import "testing"

// Stream HLS hasil resolve sering memakai URL upstream tanpa ekstensi, jadi
// jenis konten ditentukan juga dari ekstensi slug. Tanpa itu playlist tidak
// ditulis ulang ke proxy dan menu kualitas tidak termuat.
func TestPlaylistAndSubtitleDetection(t *testing.T) {
	if !isPlaylist("application/octet-stream", "/media/play", "wm-hd.m3u8") {
		t.Fatal("slug .m3u8 harus dianggap playlist")
	}
	if !isPlaylist("", "/master", "seg-abc.m3u8") {
		t.Fatal("playlist turunan harus dikenali")
	}
	if isPlaylist("video/mp4", "/movie.mp4", "wm-hd.mp4") {
		t.Fatal("mp4 bukan playlist")
	}
	if !isSubtitle("application/octet-stream", "/file", "miruro-sub-en.vtt") {
		t.Fatal("slug .vtt harus dianggap subtitle")
	}
	if isSubtitle("video/mp4", "/movie.mp4", "wm-hd.mp4") {
		t.Fatal("mp4 bukan subtitle")
	}
}
