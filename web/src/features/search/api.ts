import { api } from '../../lib/api';
import type { Anime } from '../../lib/types';

export function searchAnime(q: string): Promise<Anime[]> {
  return api<Anime[]>(`/search?q=${encodeURIComponent(q)}`);
}
