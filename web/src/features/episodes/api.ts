import { get } from '../../lib/api';
import type { AnimeDetail, Episode, Relation } from './types';

export function fetchEpisodes(id: string): Promise<Episode[]> {
  return get<Episode[]>(`/api/episodes?id=${encodeURIComponent(id)}`);
}

export function fetchDetail(id: string): Promise<AnimeDetail> {
  return get<AnimeDetail>(`/api/anime?id=${encodeURIComponent(id)}&section=base`);
}

export async function fetchRelations(id: string): Promise<Relation[]> {
  const v = await get<{ relations?: Relation[] }>(`/api/anime?id=${encodeURIComponent(id)}&section=relations`);
  return v.relations ?? [];
}
