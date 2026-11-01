// api(): GET JSON ke backend; error dibacakan dari body.error bila ada.
// `signal` dipakai saat request lama harus dibatalkan (mis. user ganti pilihan).
export async function api<T = Record<string, any>>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, signal ? { signal } : undefined)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((body as { error?: string }).error || `HTTP ${res.status}`)
  return body as T
}
