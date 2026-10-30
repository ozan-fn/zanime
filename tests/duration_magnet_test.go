package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/gravity-zero/mkvgo/matroska"
)

// Magnet Spider-Man yang diminta user. Tracker publik ditambahkan agar
// penemuan peer cepat; infoHash tidak berubah.
const spiderManMagnet = "magnet:?xt=urn:btih:26f1ce29e36b0b4b72a1b0974776af082f97fcb0&dn=Spider-Man.Brand.New.Day.2026.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir.mkv" +
	"&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce" +
	"&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce" +
	"&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce" +
	"&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce" +
	"&tr=udp%3A%2F%2Fopen.demonii.com%3A1337%2Fannounce" +
	"&tr=https%3A%2F%2Ftracker.tamersunion.org%3A443%2Fannounce"

// TestDurationMagnetHeadOnly mengukur biaya unduh dan akurasi durasi lewat
// matroska.ReadMeta + WithoutTailScan: hanya header MKV (Info.Duration) yang
// dibaca, tidak ada SeekHead/Cues/ekor. Env-gated karena butuh swarm live:
//
//	ZANIME_DURATION_MAGNET=1 go test -run DurationMagnet -timeout 240s ./tests
//
// ZANIME_MAGNET menimpa magnet. Bila swarm tidak menjawab, test skip (bukan gagal).
func TestDurationMagnetHeadOnly(t *testing.T) {
	if os.Getenv("ZANIME_DURATION_MAGNET") == "" {
		t.Skip("set ZANIME_DURATION_MAGNET=1 to probe the live magnet")
	}
	magnet := spiderManMagnet
	if v := os.Getenv("ZANIME_MAGNET"); v != "" {
		magnet = v
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.Seed = false
	cfg.NoDefaultPortForwarding = true
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("torrent client: %v", err)
	}
	defer cl.Close()

	tt, err := cl.AddMagnet(magnet)
	if err != nil {
		t.Fatalf("add magnet: %v", err)
	}
	select {
	case <-tt.GotInfo():
	case <-time.After(45 * time.Second):
		t.Skip("metadata belum tersedia dari swarm dalam 45 detik")
	}

	var f *torrent.File
	for _, x := range tt.Files() {
		if x.Length() > 0 && (f == nil || x.Length() > f.Length()) {
			f = x
		}
	}
	if f == nil {
		t.Fatal("torrent tanpa file")
	}

	deadline := time.Now().Add(15 * time.Second)
	for tt.Stats().ActivePeers == 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	st := tt.Stats()
	t.Logf("peers: aktif=%d seeder=%d total=%d | piece=%.1f MiB | ukuran_file=%.2f GiB",
		st.ActivePeers, st.ConnectedSeeders, st.TotalPeers,
		float64(tt.Info().PieceLength)/(1<<20), float64(f.Length())/(1<<30))

	before := st.BytesReadUsefulData.Int64()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	r := f.NewReader()
	r.SetContext(ctx)
	defer func() { _ = r.Close() }()

	c, err := matroska.ReadMeta(ctx, r, f.Path(), matroska.WithoutTailScan())
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	st = tt.Stats()
	used := st.BytesReadUsefulData.Int64() - before
	if c.DurationMs <= 0 {
		t.Fatalf("durasi 0: Info tidak memuat Duration")
	}
	// used = byte piece yang benar-benar diunduh dari peer, bukan ukuran file.
	t.Logf("durasi=%s | diunduh=%.2f MiB (%.2f%% dari file %.2f GiB)",
		time.Duration(c.DurationMs)*time.Millisecond,
		float64(used)/(1<<20), 100*float64(used)/float64(f.Length()), float64(f.Length())/(1<<30))
}
