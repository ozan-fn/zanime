//! Test logika murni `zanime`: tanpa jaringan, tanpa torrent, jalan cepat.//! Semua yang bisa gagal diam-diam (parsing range, escaping URL, filter file)
//! ada di sini supaya regresi tertangkap sebelum menyentuh swarm sungguhan.

use zanime::{is_video, magnet_hash, parse_range, pick_videos, stream_url, video_mime, RangeSpec};

#[test]
fn videos_are_filtered_and_sorted_by_name() {
    let files = vec![
        (0, "pack/ep02.mkv".to_string(), 10u64),
        (1, "pack/ep10.mkv".to_string(), 10),
        (2, "pack/ep01.mkv".to_string(), 10),
        (3, "pack/notes.txt".to_string(), 1),
        (4, "pack/Extra.MP4".to_string(), 5),
    ];
    let videos = pick_videos(files.into_iter());
    let names: Vec<&str> = videos.iter().map(|f| f.name()).collect();
    assert_eq!(
        names,
        vec!["ep01.mkv", "ep02.mkv", "ep10.mkv", "Extra.MP4"],
        "urutan memakai path huruf kecil, seperti Go"
    );
    assert_eq!(videos[0].id, 2, "indeks file torrent ikut dipertahankan");
}

#[test]
fn is_video_matches_container_extensions_case_insensitively() {
    for path in ["a.mkv", "B.MP4", "c.WebM", "d.ts"] {
        assert!(is_video(path), "{path} harus dianggap video");
    }
    for path in ["a.txt", "b.nfo", "c", "d.jpg"] {
        assert!(!is_video(path), "{path} bukan video");
    }
}

#[test]
fn stream_url_escapes_the_name() {
    assert_eq!(
        stream_url(3, "[Erai-raws] Drop Out - 03.mkv"),
        "/stream/3/%5BErai-raws%5D%20Drop%20Out%20-%2003.mkv"
    );
}

#[test]
fn range_parsing_matches_http_expectations() {
    assert_eq!(parse_range(None, 100), RangeSpec::Full);
    assert_eq!(
        parse_range(Some("bytes=0-99"), 100),
        RangeSpec::Partial { start: 0, end: 99 }
    );
    assert_eq!(
        parse_range(Some("bytes=50-"), 100),
        RangeSpec::Partial { start: 50, end: 99 }
    );
    assert_eq!(
        parse_range(Some("bytes=-10"), 100),
        RangeSpec::Partial { start: 90, end: 99 }
    );
    assert_eq!(
        parse_range(Some("bytes=0-1000"), 100),
        RangeSpec::Partial { start: 0, end: 99 }
    );
    assert_eq!(
        parse_range(Some("bytes=0-9,20-29"), 100),
        RangeSpec::Partial { start: 0, end: 9 },
        "multi-range memakai rentang pertama"
    );
    assert_eq!(
        parse_range(Some("bytes=100-"), 100),
        RangeSpec::Unsatisfiable
    );
    assert_eq!(parse_range(Some("bytes=5-1"), 100), RangeSpec::Unsatisfiable);
    assert_eq!(parse_range(Some("bytes=0-"), 0), RangeSpec::Unsatisfiable);
}

#[test]
fn video_mime_covers_every_video_extension() {
    assert_eq!(video_mime("a.mkv"), "video/x-matroska");
    assert_eq!(video_mime("a.mp4"), "video/mp4");
    assert_eq!(video_mime("a.webm"), "video/webm");
    assert_eq!(video_mime("a.avi"), "video/x-msvideo");
    assert_eq!(video_mime("a.mov"), "video/quicktime");
    assert_eq!(video_mime("a.ts"), "video/mp2t");
    assert_eq!(video_mime("a.txt"), "application/octet-stream");
}

#[test]
fn magnet_hash_accepts_only_real_magnets() {
    let magnet = "magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b";
    assert_eq!(magnet_hash(magnet).unwrap(), "3c5356a2834c80305e18479ad39d16a4f8b8682b");
    assert!(magnet_hash("bukan magnet").is_err());
}
