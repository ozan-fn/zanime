// Bentuk state yang dikirim server via /api/ws dan /api/state. Angka bisa
// hilang per frame, jadi semua field opsional; akses pakai `|| 0` dsb.
export type TorrentFile = {
  index: number
  name: string
  path?: string
  size?: number
}

export type SubtitleTrack = {
  url: string
  lang: string
  name?: string
  format?: string
}

export type TorrentState = {
  ready?: boolean
  loading?: boolean
  files?: TorrentFile[]
  selected?: number
  cache?: { used?: number; limit?: number; evictions?: number; dir?: string }
  peers?: number
  name?: string
  info_hash?: string
  length?: number
  downloaded?: number
  torrent?: {
    pieces?: number
    piece_length?: number
    complete_pieces?: number
    partial_pieces?: number
    busy_pieces?: number
    total_peers?: number
    seeders?: number
    half_open?: number
    pending_peers?: number
    chunks_useful?: number
    chunks_wasted?: number
    wire_in?: number
    hashed?: number
    wasted_bytes?: number
    data_in?: number
    ram_bytes?: number
  }
  net?: { from_peers?: number; to_client?: number }
  hls?: {
    index?: number
    ready?: boolean
    live?: boolean
    error?: string
    playlist?: string
    subs?: SubtitleTrack[]
  }
  users?: { active?: number; tabs?: number }
  server?: { uptime?: number; goroutines?: number; heap?: number; sys?: number }
}
