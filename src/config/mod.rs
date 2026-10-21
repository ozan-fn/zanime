#[derive(Debug, Clone)]
pub struct Config {
    pub bind: (String, u16),
    pub user_agent: &'static str,
    pub origin: &'static str,
    pub referer: &'static str,
    pub graphql_url: &'static str,
    pub api_base: &'static str,
    pub provider: &'static str,
    /// Satu atau banyak key (pisah koma/baris) → dipakai round-robin.
    pub llm_keys: Vec<String>,
    /// Base URL kompatibel OpenAI (env `OPENAI_BASE_URL`), mis. `https://kenari.id/v1`.
    pub llm_base: String,
    /// Model (env `OPENAI_MODEL`). Default `gemma-4-31b-it`.
    pub llm_model: String,
    /// Cache Redis (opsional): `rediss://default:<pass>@<host>:6379`. Kosong = tanpa cache.
    pub redis_url: String,
}

impl Default for Config {
    fn default() -> Self {
        let _ = dotenvy::dotenv();
        Self {
            bind: (
                "0.0.0.0".to_string(),
                std::env::var("PORT")
                    .ok()
                    .and_then(|p| p.parse().ok())
                    .unwrap_or(3000),
            ),
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
            origin: "https://anistream.one",
            referer: "https://anistream.one/",
            graphql_url: "https://graphql.animex.one/graphql",
            api_base: "https://api.anistream.one/rest/api",
            provider: "yuki",
            llm_keys: std::env::var("OPENAI_API_KEY")
                .unwrap_or_default()
                .split([',', '\n'])
                .map(str::trim)
                .filter(|s| !s.is_empty())
                .map(str::to_string)
                .collect(),
            llm_base: std::env::var("OPENAI_BASE_URL")
                .ok()
                .filter(|s| !s.trim().is_empty())
                .unwrap_or_else(|| "https://kenari.id/v1".to_string()),
            llm_model: std::env::var("OPENAI_MODEL")
                .ok()
                .filter(|s| !s.trim().is_empty())
                .unwrap_or_else(|| "gemma-4-31b-it".to_string()),
            redis_url: std::env::var("REDIS_URL").unwrap_or_default(),
        }
    }
}
