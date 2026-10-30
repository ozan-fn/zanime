// Penyimpan thumbnail di luar React: dibaca/ditulis banyak fitur (search
// dialog, file list) tanpa prop drilling, dan persist ke localStorage
// bersama magnet di fitur magnet.

import { LAST_KEY, loadLastSaved, saveLast } from '@/features/magnet/last'

export type ThumbStore = {
  epThumbs: Record<string, string>
  pickedThumb: string
}

const store: ThumbStore & { loaded?: boolean } = { epThumbs: {}, pickedThumb: '' }

export function epThumbsState(): ThumbStore {
  // Isi awal diambil dari localStorage saat modul aktif.
  if (!store.loaded) {
    const saved = loadLastSaved()
    store.epThumbs = (saved.thumbs as Record<string, string>) || {}
    store.pickedThumb = saved.thumb || ''
    store.loaded = true
  }
  return store
}

export function resetEpThumbs() {
  epThumbsState()
  store.epThumbs = {}
  store.pickedThumb = ''
  persist()
}

export function setPickedThumb(thumb: string) {
  epThumbsState()
  store.pickedThumb = thumb
  persist()
}

export function setStoreThumbs(ids: Record<string, string>) {
  epThumbsState()
  store.epThumbs = ids || {}
  persist()
}

function persist() {
  saveLast({ thumbs: store.epThumbs, thumb: store.pickedThumb })
}

export function pickedThumbOf(): string {
  return epThumbsState().pickedThumb
}

// Kunci konsumen yang sama dengan fitur magnet.
export { LAST_KEY }
