//! Deteksi/unduh ffmpeg + kebutuhan remux. Media tetap nol disk — hanya
//! binary ffmpeg (opsional) yang disimpan di ./tmp.

use std::path::{Path, PathBuf};

#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

/// ID elemen EBML `Cues` Matroska (RFC 9559) — index seek di ekor file.
pub const CUES_ID: [u8; 4] = [0x1C, 0x53, 0xBB, 0x6B];

/// Lokasi ffmpeg: `./tmp/ffmpeg` dulu, lalu `ffmpeg` dari sistem, terakhir
/// unduh build statis ke `./tmp/ffmpeg`. Hasil di-cache sekali saja.
pub async fn ensure() -> Option<PathBuf> {
    static FFMPEG: tokio::sync::OnceCell<Option<PathBuf>> = tokio::sync::OnceCell::const_new();
    FFMPEG
        .get_or_init(|| async {
            let local = Path::new("./tmp/ffmpeg");
            if local.exists() {
                return Some(local.to_path_buf());
            }
            let ok = std::process::Command::new("ffmpeg")
                .arg("-version")
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status()
                .is_ok_and(|s| s.success());
            if ok {
                return Some(PathBuf::from("ffmpeg"));
            }
            eprintln!("ffmpeg tidak ditemukan; mengunduh build statis ke ./tmp/ffmpeg …");
            match download_to_tmp().await {
                Some(path) => Some(path),
                None => {
                    eprintln!("ffmpeg gagal diunduh; remux tidak aktif");
                    None
                }
            }
        })
        .await
        .clone()
}

/// Unduh build statis ffmpeg (johnvansickle.com), ambil hanya binary
/// `ffmpeg`-nya, simpan ke ./tmp/ffmpeg, hapus arsipnya.
async fn download_to_tmp() -> Option<PathBuf> {
    let arch = if cfg!(target_arch = "aarch64") {
        "arm64"
    } else {
        "amd64"
    };
    let url =
        format!("https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-{arch}-static.tar.xz");
    let response = reqwest::get(&url).await.ok()?;
    let bytes = response.bytes().await.ok()?;
    let mut tar_data = Vec::new();
    lzma_rs::xz_decompress(&mut &bytes[..], &mut tar_data).ok()?;
    let mut archive = tar::Archive::new(&tar_data[..]);
    std::fs::create_dir_all("./tmp").ok()?;
    for entry in archive.entries().ok()? {
        let mut entry = entry.ok()?;
        let is_ffmpeg = entry
            .path()
            .ok()?
            .file_name()
            .is_some_and(|n| n == "ffmpeg");
        if is_ffmpeg && entry.header().entry_type().is_file() {
            entry.unpack("./tmp/ffmpeg").ok()?;
            #[cfg(unix)]
            {
                let _ = std::fs::set_permissions(
                    "./tmp/ffmpeg",
                    std::fs::Permissions::from_mode(0o755),
                );
            }
            // Binary valid? Kalau tidak, jangan anggap berhasil.
            let works = std::process::Command::new("./tmp/ffmpeg")
                .arg("-version")
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status()
                .is_ok_and(|s| s.success());
            return works.then_some(PathBuf::from("./tmp/ffmpeg"));
        }
    }
    None
}

/// Kontainer Matroska: MKV/WebM/MKA.
pub fn is_matroska(path: &str) -> bool {
    let ext = path
        .rsplit_once('.')
        .map(|(_, ext)| ext.to_lowercase())
        .unwrap_or_default();
    matches!(ext.as_str(), "mkv" | "webm" | "mka")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn matroska_extensions_detected() {
        assert!(is_matroska("a.mkv"));
        assert!(is_matroska("b.WEBM"));
        assert!(is_matroska("c.mka"));
        assert!(!is_matroska("d.mp4"));
        assert!(!is_matroska("e.txt"));
    }

    #[test]
    fn cues_id_is_the_rfc_value() {
        assert_eq!(CUES_ID, [0x1C, 0x53, 0xBB, 0x6B]);
    }
}
