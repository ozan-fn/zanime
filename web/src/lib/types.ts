// Tipe domain yang dipakai lintas fitur.

export interface Anime {
  id: string;
  name: string;
  poster?: string;
  synopsis?: string;
}

export interface Episode {
  id: string;
  number: string;
}

export type Mode = 'sub' | 'dub';

export interface SubtitleStatus {
  state: 'converting' | 'ready' | 'error';
  done: number;
  total: number;
  eta_seconds: number;
  error?: string;
}
