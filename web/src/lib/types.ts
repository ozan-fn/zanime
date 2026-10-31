// Bentuk data dari backend (lihat internal/tmdb dan internal/em).
export type Item = {
  id: string
  tmdb?: string
  name: string
  year?: string
  type: string
  poster?: string
}

export type Episode = {
  season: number
  episode: number
  title?: string
  thumbnail?: string
  released?: string
}

export type Meta = {
  id: string
  tmdb: string
  name: string
  year?: string
  description?: string
  runtime?: string
  genres?: string[]
  rating?: string
  poster?: string
  episodes?: Episode[]
}

export type Quality = { label: string; url: string }
export type Sub = { label: string; lang: string; url: string }

// Satu sumber stream ter-resolve (embedmaster).
export type Server = {
  name: string
  type: string // mp4 | hls
  quality?: string
  qualities?: Quality[]
  subs?: Sub[]
  url?: string
  err?: string
}
