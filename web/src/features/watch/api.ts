import { api } from '../../lib/api';
import type { Anime, Episode } from '../../lib/types';

// Detail katalog: semua yang kartu pencarian tidak bawa — judul Jepang,
// jendela tayang, durasi, status, skor MAL, studio, genre, terkait.
export interface AnimeDetails extends Anime {
  japanese?: string;
  aired?: string;
  premiered?: string;
  duration?: string;
  status?: string;
  score?: string;
  studios?: string;
  producers?: string;
  genres?: string[];
  episodeCount?: string;
  subCount?: string;
  dubCount?: string;
  related?: { id: string; name: string }[];
  recommended?: { id: string; name: string }[];
}

export const searchAnime = (q: string): Promise<Anime[]> =>
  api<Anime[]>(`/search?q=${encodeURIComponent(q)}`);

export const animeDetails = (id: string): Promise<AnimeDetails> =>
  api<AnimeDetails>(`/anime/${encodeURIComponent(id)}`);

export const animeEpisodes = (id: string): Promise<Episode[]> =>
  api<Episode[]>(`/anime/${encodeURIComponent(id)}/episodes`);
