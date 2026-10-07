use std::{future::Future, sync::OnceLock};

use redis::AsyncCommands as _;
use serde::{Serialize, de::DeserializeOwned};

use crate::error::AppError;

/// Koneksi Redis opsional. `REDIS_URL` kosong / gagal sambung → cache mati,
/// API tetap jalan normal (cache tak pernah jadi penyebab 5xx).
static CON: OnceLock<redis::aio::MultiplexedConnection> = OnceLock::new();

pub async fn init(url: &str) {
    if url.is_empty() {
        log::info!("cache: REDIS_URL kosong → tanpa cache");
        return;
    }
    let conn = async {
        let client = redis::Client::open(url)?;
        client.get_multiplexed_async_connection().await
    }
    .await;
    match conn {
        Ok(c) => {
            let _ = CON.set(c);
            log::info!("cache: redis siap");
        }
        Err(e) => log::warn!("cache: redis gagal ({e}) → tanpa cache"),
    }
}

/// Ambil `key` dari Redis; miss / isi rusak / redis error → jalankan `f`,
/// lalu simpan hasilnya dengan TTL `ttl` detik (best-effort, gagal = diabaikan).
pub async fn cached<T, F>(key: String, ttl: u64, f: F) -> Result<T, AppError>
where
    T: Serialize + DeserializeOwned,
    F: Future<Output = Result<T, AppError>>,
{
    let Some(con) = CON.get() else { return f.await };
    let mut c = con.clone();

    if let Ok(Some(s)) = c.get::<_, Option<String>>(&key).await
        && let Ok(v) = serde_json::from_str(&s)
    {
        return Ok(v);
    }

    let v = f.await?;
    // `null` tidak disimpan: endpoint memakainya untuk "tak ada data" — dan query yang
    // rusak juga jatuh ke sana. Kalau ikut di-cache, hasilnya lengket selama TTL sehari.
    if let Ok(s) = serde_json::to_string(&v)
        && s != "null"
    {
        let _: Result<(), _> = c.set_ex(&key, s, ttl).await;
    }
    Ok(v)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Tanpa redis (`CON` belum diisi) hasil upstream harus lewat apa adanya.
    #[tokio::test]
    async fn tanpa_redis_lewat_langsung() {
        let v: u32 = cached("kunci".to_string(), 1, async { Ok::<_, AppError>(9) }).await.unwrap();
        assert_eq!(v, 9);
    }
}
