// api(): GET JSON ke backend; error dibacakan dari body.error bila ada.
export async function api<T = Record<string, any>>(path: string): Promise<T> {
  const res = await fetch(path)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((body as { error?: string }).error || `HTTP ${res.status}`)
  return body as T
}
