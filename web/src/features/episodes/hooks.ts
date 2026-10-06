import { fetchDetail, fetchEpisodes, fetchRelations } from './api';
import { detail, episodes, error, loading, relations } from './stores';

export async function loadAnime(id: string): Promise<void> {
  loading.set(true);
  error.set('');
  episodes.set([]);
  detail.set(null);
  relations.set([]);
  try {
    const [d, e, r] = await Promise.all([
      fetchDetail(id),
      fetchEpisodes(id).catch(() => []),
      fetchRelations(id).catch(() => []),
    ]);
    detail.set(d);
    episodes.set(e);
    relations.set(r);
  } catch (e) {
    error.set(e instanceof Error ? e.message : 'gagal memuat');
  } finally {
    loading.set(false);
  }
}
