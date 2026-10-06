import { searchAnime } from './api';
import { error, lastQuery, loading, results } from './stores';

export async function runSearch(query: string): Promise<void> {
  const q = query.trim();
  if (!q) return;
  loading.set(true);
  error.set('');
  lastQuery.set(q);
  try {
    results.set(await searchAnime(q));
  } catch (e) {
    error.set(e instanceof Error ? e.message : 'gagal mencari');
    results.set([]);
  } finally {
    loading.set(false);
  }
}

export function clearSearch(): void {
  results.set([]);
  error.set('');
  lastQuery.set('');
}
