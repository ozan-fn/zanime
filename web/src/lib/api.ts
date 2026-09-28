// Helper fetch dasar: semua endpoint Go hidup di bawah /api/, jadi proxy dev
// cukup satu entri dan Go satu subtree. Body error JSON dibuka agar pesan
// upstream terbaca di UI.
export async function api<T = unknown>(path: string): Promise<T> {
  const r = await fetch('/api' + path);
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error((body as { error?: string }).error || r.statusText);
  return body as T;
}
