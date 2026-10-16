module models

// Output web (klien menguji nama key). Katalog upstream tetap diteruskan sebagai
// json2.Any (tanpa struct perantara) — cukup AnimeOut untuk kartu yang dipetakan.
pub struct AnimeOut {
pub:
	id            string
	anilist_id    i64     @[json: 'anilistId']
	title_romaji  string  @[json: 'titleRomaji']
	title_english ?string @[json: 'titleEnglish']
	episode_count ?i64    @[json: 'episodeCount']
	cover         string
	banner        string
	format        ?string
	season_year   ?i64 @[json: 'seasonYear']
	average_score ?i64 @[json: 'averageScore']
	status        ?string
}

pub struct EpisodeItem {
pub:
	number       i64
	title        string
	title_romaji string @[json: 'titleRomaji']
	title_jp     string @[json: 'titleJp']
	img          string
}

pub struct StreamResponse {
pub:
	stream string
	sub_en string
	sub_id string
}

pub struct SubStatus {
pub mut:
	state       string
	queue       int
	queue_total int
	eta_sec     u64
	done        int
	total       int
	next_at     f64
	error       ?string
	// Delta VTT cue yang baru diterjemahkan di call ini (kosong bila tidak ada).
	vtt string
}
