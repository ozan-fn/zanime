import { get } from '../../lib/api';
import type { Anime } from './types';

export function searchAnime(query: string): Promise<Anime[]> {
  return get<Anime[]>(`/api/search?query=${encodeURIComponent(query)}`);
}
