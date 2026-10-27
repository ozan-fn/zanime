//! Storage RAM murni: piece di atas batas dibuang (LRU) dan ditandai "belum
//! punya" lewat librqbit yang dipatch, jadi bisa diunduh ulang saat di-seek.
use anyhow::{Context, Result};
use librqbit::storage::{BoxStorageFactory, StorageFactory, StorageFactoryExt, TorrentStorage};
use librqbit::{ManagedTorrentShared, TorrentMetadata};
use std::collections::HashMap;
use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use tokio::sync::mpsc::UnboundedSender;

/// Angka cache yang dipakai `/api/ws` dan `/info`.
#[derive(Default)]
pub struct Stats {
    used: AtomicU64,
    evictions: AtomicU64,
}

impl Stats {
    pub fn used(&self) -> u64 {
        self.used.load(Ordering::Relaxed)
    }

    pub fn evictions(&self) -> u64 {
        self.evictions.load(Ordering::Relaxed)
    }
}

pub struct MemStorageFactory {
    pub limit: u64,
    pub evicted: UnboundedSender<u32>,
    pub stats: Arc<Stats>,
}

impl Clone for MemStorageFactory {
    fn clone(&self) -> Self {
        Self {
            limit: self.limit,
            evicted: self.evicted.clone(),
            stats: self.stats.clone(),
        }
    }
}

impl StorageFactory for MemStorageFactory {
    type Storage = MemStorage;

    fn create(&self, _: &ManagedTorrentShared, metadata: &TorrentMetadata) -> Result<MemStorage> {
        Ok(MemStorage::new(
            metadata.lengths.default_piece_length().max(1) as u64,
            metadata
                .file_infos
                .iter()
                .map(|f| f.offset_in_torrent)
                .collect(),
            self.limit
                .max(metadata.lengths.default_piece_length() as u64),
            self.evicted.clone(),
            self.stats.clone(),
        ))
    }

    fn clone_box(&self) -> BoxStorageFactory {
        self.clone().boxed()
    }
}

#[derive(Default)]
struct State {
    pieces: HashMap<u32, (Vec<u8>, u64)>,
    bytes: u64,
    tick: u64,
}

struct Inner {
    piece_len: u64,
    offsets: Vec<u64>,
    limit: u64,
    evicted: UnboundedSender<u32>,
    stats: Arc<Stats>,
    state: Mutex<State>,
}

impl Inner {
    fn at(&self, file_id: usize, offset: u64) -> Result<u64> {
        Ok(*self.offsets.get(file_id).context("file_id invalid")? + offset)
    }

    fn touch<'a>(&self, st: &'a mut State, pid: u32) -> &'a mut Vec<u8> {
        st.tick += 1;
        if !st.pieces.contains_key(&pid) {
            while st.bytes + self.piece_len > self.limit && !st.pieces.is_empty() {
                let victim = st
                    .pieces
                    .iter()
                    .min_by_key(|(_, (_, used))| *used)
                    .map(|(id, _)| *id)
                    .unwrap();
                if let Some((old, _)) = st.pieces.remove(&victim) {
                    st.bytes -= old.len() as u64;
                    self.stats.evictions.fetch_add(1, Ordering::Relaxed);
                    let _ = self.evicted.send(victim);
                }
            }
            st.bytes += self.piece_len;
            self.stats.used.store(st.bytes, Ordering::Relaxed);
            st.pieces
                .insert(pid, (Vec::with_capacity(self.piece_len as usize), st.tick));
        }
        let tick = st.tick;
        let piece = st.pieces.get_mut(&pid).unwrap();
        piece.1 = tick;
        &mut piece.0
    }
}

#[derive(Clone)]
pub struct MemStorage {
    inner: Arc<Inner>,
}

impl MemStorage {
    pub(crate) fn new(
        piece_len: u64,
        offsets: Vec<u64>,
        limit: u64,
        evicted: UnboundedSender<u32>,
        stats: Arc<Stats>,
    ) -> Self {
        Self {
            inner: Arc::new(Inner {
                piece_len,
                offsets,
                limit,
                evicted,
                stats,
                state: Mutex::new(State::default()),
            }),
        }
    }
}

impl TorrentStorage for MemStorage {
    fn init(&mut self, _: &ManagedTorrentShared, _: &TorrentMetadata) -> Result<()> {
        Ok(())
    }

    fn pread_exact(&self, file_id: usize, offset: u64, buf: &mut [u8]) -> Result<()> {
        let mut abs = self.inner.at(file_id, offset)?;
        let mut done = 0;
        let mut st = self.inner.state.lock().unwrap();
        st.tick += 1;
        while done < buf.len() {
            let pid = (abs / self.inner.piece_len) as u32;
            let off = (abs % self.inner.piece_len) as usize;
            let tick = st.tick;
            let Some((piece, used)) = st.pieces.get_mut(&pid) else {
                anyhow::bail!("piece {pid} sudah dibuang dari RAM");
            };
            *used = tick;
            let n = (buf.len() - done).min(piece.len().saturating_sub(off));
            if n == 0 {
                anyhow::bail!("piece {pid} belum lengkap");
            }
            buf[done..done + n].copy_from_slice(&piece[off..off + n]);
            done += n;
            abs += n as u64;
        }
        Ok(())
    }

    fn pwrite_all(&self, file_id: usize, offset: u64, buf: &[u8]) -> Result<()> {
        let piece_len = self.inner.piece_len;
        let mut abs = self.inner.at(file_id, offset)?;
        let mut done = 0;
        let mut st = self.inner.state.lock().unwrap();
        while done < buf.len() {
            let pid = (abs / piece_len) as u32;
            let off = (abs % piece_len) as usize;
            let n = (buf.len() - done).min(piece_len as usize - off);
            let piece = self.inner.touch(&mut st, pid);
            if piece.len() < off + n {
                piece.resize(off + n, 0);
            }
            piece[off..off + n].copy_from_slice(&buf[done..done + n]);
            done += n;
            abs += n as u64;
        }
        Ok(())
    }

    fn remove_file(&self, _: usize, _: &Path) -> Result<()> {
        Ok(())
    }

    fn remove_directory_if_empty(&self, _: &Path) -> Result<()> {
        Ok(())
    }

    fn ensure_file_length(&self, _: usize, _: u64) -> Result<()> {
        Ok(())
    }

    fn take(&self) -> Result<Box<dyn TorrentStorage>> {
        Ok(Box::new(self.clone()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn storage(
        limit: u64,
    ) -> (
        MemStorage,
        tokio::sync::mpsc::UnboundedReceiver<u32>,
        Arc<Stats>,
    ) {
        let (tx, rx) = tokio::sync::mpsc::unbounded_channel();
        let stats = Arc::new(Stats::default());
        let storage = MemStorage::new(4, vec![0], limit, tx, stats.clone());
        (storage, rx, stats)
    }

    #[test]
    fn evicts_least_recently_used_piece_and_refetches_it() {
        let (storage, mut evicted, stats) = storage(8);
        storage.pwrite_all(0, 0, &[1; 4]).unwrap();
        storage.pwrite_all(0, 4, &[2; 4]).unwrap();

        // Piece 1 dipakai lagi, jadi piece 0 yang paling lama tidak tersentuh.
        let mut buf = [0u8; 4];
        storage.pread_exact(0, 4, &mut buf).unwrap();
        assert_eq!(buf, [2; 4]);

        // Piece ketiga memaksa eviction; piece 0 yang paling lama tidak dipakai.
        storage.pwrite_all(0, 8, &[3; 4]).unwrap();
        assert_eq!(evicted.try_recv().unwrap(), 0);
        assert!(storage.pread_exact(0, 0, &mut buf).is_err());
        assert_eq!(stats.evictions(), 1);
        assert_eq!(stats.used(), 8);

        // Byte yang sama datang lagi: harus bisa ditulis ulang lalu dibaca.
        storage.pwrite_all(0, 0, &[9; 4]).unwrap();
        storage.pread_exact(0, 0, &mut buf).unwrap();
        assert_eq!(buf, [9; 4]);
    }

    #[test]
    fn single_piece_larger_than_limit_is_still_stored() {
        let (storage, _, _) = storage(4);
        storage.pwrite_all(0, 0, &[7; 4]).unwrap();
        let mut buf = [0u8; 4];
        storage.pread_exact(0, 0, &mut buf).unwrap();
        assert_eq!(buf, [7; 4]);
    }
}
