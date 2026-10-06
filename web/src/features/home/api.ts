import { get } from '../../lib/api';
import type { Anime } from '../search/types';

export type CatalogParams = Record<string, string | number>;

/** Katalog home. Parameter diteruskan apa adanya ke /api/catalog. */
export function fetchCatalog(params: CatalogParams): Promise<Anime[]> {
  const qs = new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]));
  return get<Anime[]>(`/api/catalog?${qs}`);
}
