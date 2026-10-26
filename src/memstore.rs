// Storage RAM murni: piece > mem_limit dibuang (LRU) dan ditandai "belum punya"
// lewat librqbit yang sudah dipatch, jadi bisa diunduh ulang saat di-seek.
use anyhow::{Context, Result};
use librqbit::storage::{BoxStorageFactory, StorageFactory, StorageFactoryExt, TorrentStorage};
use librqbit::{ManagedTorrentShared, TorrentMetadata};
use std::collections::HashMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use tokio::sync::mpsc::UnboundedSender;

pub struct MemStorageFactory {
    pub limit: u64,
    pub evicted: UnboundedSender<u32>,
}

impl Clone for MemStorageFactory {
    fn clone(&self) -> Self {
        Self {
            limit: self.limit,
            evicted: self.evicted.clone(),
        }
    }
}

impl StorageFactory for MemStorageFactory {
    type Storage = MemStorage;

    fn create(&self, _: &ManagedTorrentShared, metadata: &TorrentMetadata) -> Result<MemStorage> {
        Ok(MemStorage {
            inner: Arc::new(Inner {
                piece_len: metadata.lengths.default_piece_length().max(1) as u64,
                offsets: metadata
                    .file_infos
                    .iter()
                    .map(|f| f.offset_in_torrent)
                    .collect(),
                limit: self
                    .limit
                    .max(metadata.lengths.default_piece_length() as u64),
                evicted: self.evicted.clone(),
                state: Mutex::new(State::default()),
            }),
        })
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
                    let _ = self.evicted.send(victim);
                }
            }
            st.bytes += self.piece_len;
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
