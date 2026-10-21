import { fetchStream } from './api';
import { error, loading, stream } from './stores';

export async function loadStream(id: string, ep: number): Promise<void> {
  loading.set(true);
  error.set('');
  // Sengaja tidak mengosongkan `stream`: player lama tetap ter-mount selama episode
  // berikutnya dimuat, jadi fullscreen tak putus di auto next.
  try {
    stream.set(await fetchStream(id, ep));
  } catch (e) {
    error.set(e instanceof Error ? e.message : 'gagal memuat stream');
  } finally {
    loading.set(false);
  }
}
