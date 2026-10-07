use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Deserialize)]
pub struct SearchQuery {
    pub query: String,
}

#[derive(Debug, Deserialize)]
pub struct EpisodesQuery {
    pub id: String,
}

/// Katalog: filter + sort upstream (`AnimeCatalogFilterInput`/`AnimeSortInput`).
#[derive(Debug, Deserialize)]
pub struct CatalogQuery {
    pub sort: Option<String>,
    pub direction: Option<String>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
    pub status: Option<String>,
    pub format: Option<String>,
    pub season: Option<String>,
    pub year: Option<i64>,
    pub genre: Option<String>,
    pub query: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ServersQuery {
    pub id: String,
    pub ep: i64,
}

#[derive(Debug, Deserialize)]
pub struct SourcesQuery {
    pub id: String,
    pub ep: i64,
    #[serde(default = "def_sub")]
    pub r#type: String,
    #[serde(default = "def_yuki")]
    pub provider: String,
}

fn def_sub() -> String {
    "sub".to_string()
}
fn def_yuki() -> String {
    "yuki".to_string()
}

pub type StreamQuery = ServersQuery;

#[derive(Debug, Deserialize)]
pub struct AnimeQuery {
    pub id: Option<String>,
    #[serde(rename = "anilistId")]
    pub anilist_id: Option<i64>,
    #[serde(default = "def_section")]
    pub section: String,
}

fn def_section() -> String {
    "base".to_string()
}

impl AnimeQuery {
    pub fn id(&self) -> Option<&str> {
        self.id.as_deref()
    }
    pub fn anilist_id(&self) -> Option<i64> {
        self.anilist_id
    }
}

#[derive(Debug, Deserialize)]
pub struct SkiptimesQuery {
    pub mal: i64,
    pub ep: i64,
    pub len: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct CommentsQuery {
    pub media: String,
    #[serde(default)]
    pub offset: u32,
    #[serde(default = "def_sort")]
    pub sort: String,
    #[serde(default = "def_limit")]
    pub limit: u32,
}

fn def_sort() -> String {
    "Top".to_string()
}
fn def_limit() -> u32 {
    10
}

#[derive(Debug, Deserialize)]
pub struct EpisodeMetaQuery {
    #[serde(rename = "anilistId")]
    pub anilist_id: i64,
    pub ep: i64,
}

#[derive(Debug, Deserialize)]
pub struct AnilistBody {
    pub query: String,
    pub variables: Option<Value>,
}

#[derive(Debug, Deserialize)]
pub struct ProxyQuery {
    pub u: String,
    pub origin: Option<String>,
    pub referer: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct AnimeItem {
    pub id: String,
    #[serde(rename = "anilistId")]
    pub anilist_id: i64,
    #[serde(rename = "titleRomaji")]
    pub title_romaji: String,
    #[serde(rename = "titleEnglish")]
    pub title_english: Option<String>,
    #[serde(rename = "episodeCount")]
    pub episode_count: Option<i64>,
    #[serde(rename = "coverImage", default)]
    pub cover_image: Value,
    #[serde(rename = "bannerImage", default)]
    pub banner_image: Value,
    #[serde(default)]
    pub format: Option<String>,
    #[serde(rename = "seasonYear", default)]
    pub season_year: Option<i64>,
    #[serde(rename = "averageScore", default)]
    pub average_score: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct AnimeOut {
    pub id: String,
    #[serde(rename = "anilistId")]
    pub anilist_id: i64,
    #[serde(rename = "titleRomaji")]
    pub title_romaji: String,
    #[serde(rename = "titleEnglish")]
    pub title_english: Option<String>,
    #[serde(rename = "episodeCount")]
    pub episode_count: Option<i64>,
    pub cover: String,
    pub banner: String,
    pub format: Option<String>,
    #[serde(rename = "seasonYear")]
    pub season_year: Option<i64>,
    #[serde(rename = "averageScore")]
    pub average_score: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct EpisodeItem {
    pub number: i64,
    pub title: String,
    pub img: String,
}

#[derive(Debug, Serialize)]
pub struct StreamResponse {
    pub stream: String,
    pub sub_en: String,
    pub sub_id: String,
}

#[derive(Debug, Deserialize)]
pub struct SubIdQuery {
    pub id: String,
    pub at: Option<f64>,
    pub only: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct SubBatchReq {
    pub id: String,
    /// Posisi putar (detik) saat sekarang — batch diambil di sekitar sini.
    pub at: Option<f64>,
}

#[derive(Debug, Serialize)]
pub struct SubStatus {
    pub state: String,
    pub queue: usize,
    /// Total job masih antre; `queue` = posisi job ini.
    pub queue_total: usize,
    /// Estimasi sisa detik sebelum job ini mulai diterjemahkan.
    pub eta_sec: u64,
    pub done: usize,
    pub total: usize,
    pub next_at: f64,
    pub error: Option<String>,
    /// Delta VTT cue yang baru diterjemahkan di call ini (kosong bila tidak ada).
    #[serde(default)]
    pub vtt: String,
}
