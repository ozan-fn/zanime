//! Deteksi/unduh ffmpeg. Media tetap nol disk — hanya binary ffmpeg
//! (opsional) yang disimpan di ./tmp. Jalan di Linux dan Windows.

use std::path::{Path, PathBuf};

#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

/// Lokasi binary ffmpeg lokal, mengikuti konvensi nama tiap OS.
#[cfg(windows)]
const LOCAL: &str = "./tmp/ffmpeg.exe";
#[cfg(not(windows))]
const LOCAL: &str = "./tmp/ffmpeg";

/// Lokasi ffmpeg: binary lokal dulu, lalu `ffmpeg` dari sistem, terakhir
/// unduh build statis ke `./tmp`. Hasil di-cache sekali saja.
pub async fn ensure() -> Option<PathBuf> {
    static FFMPEG: tokio::sync::OnceCell<Option<PathBuf>> = tokio::sync::OnceCell::const_new();
    FFMPEG
        .get_or_init(|| async {
            if Path::new(LOCAL).exists() {
                return Some(PathBuf::from(LOCAL));
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
            eprintln!("ffmpeg tidak ditemukan; mengunduh build statis ke {LOCAL} …");
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

/// Binary valid? Kalau tidak, jangan anggap berhasil.
fn works(path: &str) -> bool {
    std::process::Command::new(path)
        .arg("-version")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .is_ok_and(|s| s.success())
}

/// Unduh build statis ffmpeg (gyan.dev), ambil hanya `bin/ffmpeg.exe`,
/// simpan ke `./tmp`, hapus arsipnya.
#[cfg(windows)]
async fn download_to_tmp() -> Option<PathBuf> {
    const URL: &str = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip";
    let bytes = reqwest::get(URL).await.ok()?.bytes().await.ok()?;
    let mut archive = zip::ZipArchive::new(std::io::Cursor::new(&bytes[..])).ok()?;
    std::fs::create_dir_all("./tmp").ok()?;
    for i in 0..archive.len() {
        let mut entry = archive.by_index(i).ok()?;
        if entry.name().ends_with("bin/ffmpeg.exe") && entry.is_file() {
            entry.unpack(LOCAL).ok()?;
            return works(LOCAL).then_some(PathBuf::from(LOCAL));
        }
    }
    None
}

/// Unduh build statis ffmpeg (johnvansickle.com), ambil hanya binary
/// `ffmpeg`-nya, simpan ke `./tmp`, hapus arsipnya.
#[cfg(not(windows))]
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
            entry.unpack(LOCAL).ok()?;
            let _ = std::fs::set_permissions(LOCAL, std::fs::Permissions::from_mode(0o755));
            return works(LOCAL).then_some(PathBuf::from(LOCAL));
        }
    }
    None
}
