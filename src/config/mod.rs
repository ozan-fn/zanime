#[derive(Debug, Clone)]
pub struct Config {
    pub bind: (String, u16),
    pub user_agent: &'static str,
    pub origin: &'static str,
    pub referer: &'static str,
    pub graphql_url: &'static str,
    pub api_base: &'static str,
    pub provider: &'static str,
    pub groq_key: String,
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
            groq_key: std::env::var("GROQ_API_KEY").unwrap_or_default(),
        }
    }
}
