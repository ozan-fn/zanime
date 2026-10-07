use std::sync::atomic::{AtomicU64, Ordering};

/// Resource proses untuk navbar: diisi tiap 2 dtk oleh loop di `main`, dibaca `/api/stats`.
/// CPU disimpan sebagai persen × 10 (bilangan bulat) supaya cukup atomik tanpa lock.
static MEM: AtomicU64 = AtomicU64::new(0);
static PEAK_MEM: AtomicU64 = AtomicU64::new(0);
static CPU: AtomicU64 = AtomicU64::new(0);
static PEAK_CPU: AtomicU64 = AtomicU64::new(0);

pub fn set(mem_bytes: u64, cpu: f64) {
    let cpu = if cpu.is_finite() && cpu > 0.0 { (cpu * 10.0) as u64 } else { 0 };
    MEM.store(mem_bytes, Ordering::Relaxed);
    CPU.store(cpu, Ordering::Relaxed);
    PEAK_MEM.fetch_max(mem_bytes, Ordering::Relaxed);
    PEAK_CPU.fetch_max(cpu, Ordering::Relaxed);
}

/// (mem MB, puncak mem MB, cpu %, puncak cpu %).
pub fn snapshot() -> (u64, u64, f64, f64) {
    (
        MEM.load(Ordering::Relaxed) / 1024 / 1024,
        PEAK_MEM.load(Ordering::Relaxed) / 1024 / 1024,
        CPU.load(Ordering::Relaxed) as f64 / 10.0,
        PEAK_CPU.load(Ordering::Relaxed) as f64 / 10.0,
    )
}
