use std::fmt;

use actix_web::{HttpResponse, ResponseError, http::StatusCode};

#[derive(Debug)]
pub enum AppError {
    Upstream(String),
    NotFound(String),
    BadRequest(String),
    /// Rate-limit penerus (Gemini 429/RESOURCE_EXHAUSTED) → 429 ke klien, bukan 502.
    RateLimited(String),
}

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Upstream(e) => write!(f, "upstream error: {e}"),
            Self::NotFound(e) => write!(f, "{e}"),
            Self::BadRequest(e) => write!(f, "{e}"),
            Self::RateLimited(e) => write!(f, "{e}"),
        }
    }
}

impl std::error::Error for AppError {}

impl ResponseError for AppError {
    fn status_code(&self) -> StatusCode {
        match self {
            Self::Upstream(_) => StatusCode::BAD_GATEWAY,
            Self::NotFound(_) => StatusCode::NOT_FOUND,
            Self::BadRequest(_) => StatusCode::BAD_REQUEST,
            Self::RateLimited(_) => StatusCode::TOO_MANY_REQUESTS,
        }
    }

    fn error_response(&self) -> HttpResponse {
        HttpResponse::build(self.status_code())
            .json(serde_json::json!({ "error": self.to_string() }))
    }
}
