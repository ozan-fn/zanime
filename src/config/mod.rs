#[derive(Debug, Clone)]
pub struct Config {
    pub bind: (String, u16),
    pub user_agent: &'static str,
    pub origin: &'static str,
    pub referer: &'static str,
    pub graphql_url: &'static str,
    pub api_base: &'static str,
    pub provider: &'static str,
    pub mistral_key: String,
    /// Cache Redis (opsional): `rediss://default:<pass>@<host>:6379`. Kosong = tanpa cache.
    pub redis_url: String,
}

impl Default for Config {
    fn default() -> Self {
        let _ = dotenvy::dotenv();
        Self {
            bind: ("127.0.0.1".to_string(), 3000),
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
            origin: "https://anistream.one",
            referer: "https://anistream.one/",
            graphql_url: "https://graphql.animex.one/graphql",
            api_base: "https://api.anistream.one/rest/api",
            provider: "yuki",
            mistral_key: std::env::var("MISTRAL_API_KEY").unwrap_or_default(),
            redis_url: std::env::var("REDIS_URL").unwrap_or_default(),
        }
    }
}
