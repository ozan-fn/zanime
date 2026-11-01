package subdl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Potongan HTML asli dari subdl.com.har (atribut Astro di-escape seperti aslinya).
const seasonsPage = `<div data-page="{&quot;type&quot;:[0,&quot;tv&quot;],&quot;seasons&quot;:[1,[[0,{&quot;number&quot;:[0,&quot;specials-season&quot;],&quot;name&quot;:[0,&quot;Specials&quot;]}],[0,{&quot;number&quot;:[0,&quot;first-season&quot;],&quot;name&quot;:[0,&quot;Season 1&quot;]}],[0,{&quot;number&quot;:[0,&quot;second-season&quot;],&quot;name&quot;:[0,&quot;Season 2&quot;]}]]]}"></div>`

const tvPage = `<div data-language="english" data-language-name="English">
<li class="flex" data-n-id="hIFI0BSR5N" data-downloads="1769" data-row data-id="3543430" data-date="1768575843000" data-episode-from="1" data-episode-to="1" data-full-season data-uploader="642">
<a data-subtitle-title href="/s/info/hIFI0BSR5N/x"><h4>Frieren.Beyond.Journeys.End.S02E01.CR.WEB-DL.English.EN</h4></a>
<a data-subtitle-author href="/u/Abu3safeer"> (Abu3safeer) </a>
<a data-subtitle-download href="https://dl.subdl.com/subtitle/3543430-8465516.zip">dl</a>
</li>
<li class="flex" data-n-id="JXkX3C3yrof" data-downloads="58" data-row data-id="3573328" data-date="1775631014000" data-episode-from="2" data-episode-to="2">
<a data-subtitle-title href="/s/info/JXkX3C3yrof/x"><h4>Frieren S02E02 English</h4></a>
<a data-subtitle-download href="https://dl.subdl.com/subtitle/3573328-1.zip">dl</a>
</li>
<li class="flex" data-n-id="0scZoTPZtn" data-downloads="966" data-row data-id="3570181" data-episode-from="1" data-episode-to="10">
<a data-subtitle-title href="/s/info/x/x"><h4>Frieren S02 Pack English</h4></a>
<a data-subtitle-download href="https://dl.subdl.com/subtitle/3570181-2.zip">dl</a>
</li>
</div><div data-language="indonesian" data-language-name="Indonesian">
<li class="flex" data-n-id="a" data-downloads="5" data-row data-id="999" data-episode-from="0" data-episode-to>
<a data-subtitle-title href="/s/info/a/x"><h4>Frieren S02 Pack Indonesian</h4></a>
<a data-subtitle-download href="https://dl.subdl.com/subtitle/999-9.zip">dl</a>
</li></div>`

const moviePage = `<div data-language="english" data-language-name="English">
<li class="flex" data-n-id="m1" data-downloads="42" data-row data-id="555">
<a data-subtitle-title href="/s/info/m1/x"><h4>Hoppers 2026 English</h4></a>
<a data-subtitle-download href="https://dl.subdl.com/subtitle/555-1.zip">dl</a>
</li></div>`

// TestPickTitle: tab movie/tv menentukan jenis hasil, nama sama menang, tahun
// memutuskan saat nama sama (judul remake).
func TestPickTitle(t *testing.T) {
	hits := []Result{
		{Type: "movie", Name: "Hoppers", Year: 2026, Link: "/subtitle/sd12685588/hoppers"},
		{Type: "movie", Name: "Hoppers", Year: 1996, Link: "/subtitle/sd2/hoppers-1996"},
		{Type: "movie", Name: "Time Hoppers: The Silk Road", Year: 2025, Link: "/subtitle/sd3/x"},
		{Type: "tv", Name: "De Hoppers", Year: 2019, Link: "/subtitle/sd1326063/de-hoppers"},
	}
	cases := []struct {
		tab, name, year, want string
		ok                    bool
	}{
		{"movie", "Hoppers", "2026", "/subtitle/sd12685588/hoppers", true},
		{"movie", "hoppers", "1996", "/subtitle/sd2/hoppers-1996", true},
		{"tv", "De Hoppers", "2019", "/subtitle/sd1326063/de-hoppers", true},
		{"tv", "Hoppers", "", "/subtitle/sd1326063/de-hoppers", true}, // fallback: satu-satunya tv
		{"tv", "Tidak Ada", "", "", false},
	}
	for _, c := range cases {
		got, ok := pick(hits, c.tab, c.name, c.year)
		if ok != c.ok || got.Link != c.want {
			t.Fatalf("pick(%s,%q,%s) = %q,%v; want %q,%v", c.tab, c.name, c.year, got.Link, ok, c.want, c.ok)
		}
	}
}

// TestSeasonSlug: nomor musim dipetakan ke slug halaman (termasuk Specials).
func TestSeasonSlug(t *testing.T) {
	for _, c := range []struct {
		season int
		slug   string
		ok     bool
	}{
		{0, "specials-season", true},
		{1, "first-season", true},
		{2, "second-season", true},
		{3, "", false},
	} {
		slug, ok := seasonSlug(seasonsPage, c.season)
		if slug != c.slug || ok != c.ok {
			t.Fatalf("seasonSlug(%d) = %q,%v; want %q,%v", c.season, slug, ok, c.slug, c.ok)
		}
	}
}

// TestParseRowsEpisodeFilter: episode tv hanya menyisakan baris episode itu +
// pack yang mencakupnya; movie mengambil semua baris.
func TestParseRowsEpisodeFilter(t *testing.T) {
	ep1 := parseRows(tvPage, true, 1)
	if len(ep1) != 3 {
		t.Fatalf("episode 1: %d baris, want 3 (%+v)", len(ep1), ep1)
	}
	if ep1[0].Name != "Frieren.Beyond.Journeys.End.S02E01.CR.WEB-DL.English.EN" ||
		ep1[0].Language != "English" || ep1[0].Downloads != 1769 ||
		ep1[0].Zip != "https://dl.subdl.com/subtitle/3543430-8465516.zip" {
		t.Fatalf("baris pertama salah: %+v", ep1[0])
	}
	if ep1[2].Language != "Indonesian" || ep1[2].EpisodeTo != 0 {
		t.Fatalf("pack Indonesia salah: %+v", ep1[2])
	}

	// Search ikut menyaring bahasa + relevansi judul, Indonesian dulu.
	filtered := relevant(ep1, "Frieren: Beyond Journey's End")
	if len(filtered) != 3 || filtered[0].Lang != "indonesian" || filtered[1].Lang != "english" {
		t.Fatalf("urutan/bahasa salah: %+v", filtered)
	}

	ep2 := parseRows(tvPage, true, 2)
	if len(ep2) != 3 || ep2[0].Name != "Frieren S02E02 English" {
		t.Fatalf("episode 2: %+v", ep2)
	}

	movie := parseRows(moviePage, false, 0)
	if len(movie) != 1 || movie[0].ID != "555" || movie[0].EpisodeFrom != 0 {
		t.Fatalf("movie: %+v", movie)
	}
}

const autoJSON = `{"results":[
{"type":"tv","name":"Frieren: Beyond Journey's End","year":2023,"link":"/subtitle/sd1667204/frieren-beyond-journeys-end"},
{"type":"movie","name":"Hoppers","year":2026,"link":"/subtitle/sd12685588/hoppers"}
]}`

// TestRelevant: hanya Indonesian+English, baris judul lain dibuang (regex kata
// terpanjang dari judul), judul non-latin tidak disaring nama.
func TestRelevant(t *testing.T) {
	rows := []Subtitle{
		{Lang: "english", Name: "Frieren.Beyond.Journeys.End.S01E01-04.JAPANESE.WEBRip.NF"},
		{Lang: "vietnamese", Name: "Frieren.Beyond.the.Journey's.End.S01E01-E17.WEBRip.Netflix.vi"},
		{Lang: "indonesian", Name: "[PMR] Frieren Beyond Journey's End (BD Remux 1080p AVC FLAC AAC)"},
		{Lang: "indonesian", Name: "JudasEps1-Eps14"},
		{Lang: "english", Name: "Some Other Show S01E01"},
	}
	got := relevant(rows, "Frieren: Beyond Journey's End")
	if len(got) != 2 || got[0].Name != "[PMR] Frieren Beyond Journey's End (BD Remux 1080p AVC FLAC AAC)" ||
		got[1].Name != "Frieren.Beyond.Journeys.End.S01E01-04.JAPANESE.WEBRip.NF" {
		t.Fatalf("filter judul/bahasa salah: %+v", got)
	}

	// Judul non-latin: tanpa kata kunci, hanya filter bahasa (tetap indo dulu).
	jp := relevant(rows, "葬送のフリーレン")
	if len(jp) != 4 || jp[0].Lang != "indonesian" {
		t.Fatalf("judul non-latin: %+v", jp)
	}
}

// TestSearchFresh: satu pencarian = autocomplete + halaman judul (tv: + halaman
// musim) baru; pencarian kedua tidak memakai hasil lama.
func TestSearchFresh(t *testing.T) {
	var auto, pages int32
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auto":
			atomic.AddInt32(&auto, 1)
			_, _ = w.Write([]byte(autoJSON))
		default:
			atomic.AddInt32(&pages, 1)
			paths = append(paths, r.URL.Path)
			if strings.HasSuffix(r.URL.Path, "/second-season") {
				_, _ = w.Write([]byte(tvPage))
				return
			}
			_, _ = w.Write([]byte(seasonsPage))
		}
	}))
	defer up.Close()

	c := New()
	c.SetBase(up.URL, up.URL)
	for i := 0; i < 2; i++ {
		subs, err := c.Search(context.Background(), "tv", "Frieren: Beyond Journey's End", "2023", 2, 2)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(subs) != 3 {
			t.Fatalf("subs = %d, want 3", len(subs))
		}
	}
	if got := atomic.LoadInt32(&auto); got != 2 {
		t.Fatalf("autocomplete dipanggil %d kali, want 2 (fresh tiap cari)", got)
	}
	if got := atomic.LoadInt32(&pages); got != 4 {
		t.Fatalf("halaman diambil %d kali, want 4 (judul+musim tiap cari)", got)
	}
	if !strings.Contains(strings.Join(paths, " "), "/subtitle/sd1667204/frieren-beyond-journeys-end/second-season") {
		t.Fatalf("halaman musim 2 tidak diambil: %v", paths)
	}
}

// TestSearchError: upstream gagal / judul tak ada → error, bukan daftar kosong.
func TestSearchError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auto" {
			_, _ = w.Write([]byte(`{"results":[]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer up.Close()
	c := New()
	c.SetBase(up.URL, up.URL)

	if _, err := c.Search(context.Background(), "movie", "Tidak Ada", "", 0, 0); err == nil {
		t.Fatal("judul kosong harus error")
	}
	if _, err := c.Search(context.Background(), "movie", "Hoppers", "2026", 0, 0); err == nil {
		t.Fatal("upstream 500 harus error")
	}
}
