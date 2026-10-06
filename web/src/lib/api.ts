/** Same-origin: /api/* di-proxy Vite (dev) ke backend 3000. Tanpa env. */
const API_BASE = '';

/** Absolutkan URL relatif (player menolak base URL kosong). http(s) tetap langsung. */
export function abs(u: string): string {
  return u ? new URL(u, location.origin).href : u;
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, init);
  if (!r.ok) {
    const body = (await r.json().catch(() => null)) as { error?: string } | null;
    throw new Error(body?.error ?? `http ${r.status}`);
  }
  return (await r.json()) as T;
}

export function get<T>(path: string): Promise<T> {
  return req<T>(`${API_BASE}${path}`);
}

export function post<T>(path: string, data: unknown): Promise<T> {
  return req<T>(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
}
