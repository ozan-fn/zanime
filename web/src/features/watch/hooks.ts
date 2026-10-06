import { fetchStream } from './api';
import { error, loading, stream } from './stores';

export async function loadStream(id: string, ep: number): Promise<void> {
  loading.set(true);
  error.set('');
  stream.set(null);
  try {
    stream.set(await fetchStream(id, ep));
  } catch (e) {
    error.set(e instanceof Error ? e.message : 'gagal memuat stream');
  } finally {
    loading.set(false);
  }
}
