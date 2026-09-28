// URL helpers shared across features.

export const watchHref = (animeId: string, epId: string, mode: 'sub' | 'dub') =>
  `/w/${encodeURIComponent(animeId)}/${encodeURIComponent(epId)}${mode === 'dub' ? '?mode=dub' : ''}`;

export const animeHref = (id: string) => `/a/${encodeURIComponent(id)}`;

export function normMode(m: string | null | undefined): 'sub' | 'dub' {
  return m === 'dub' ? 'dub' : 'sub';
}

// slugTitle turns "one-piece-123" into "One Piece" for the heading shown
// before the catalog request returns the real title.
export function slugTitle(id: string) {
  const words = id.replace(/-\d+$/, '').replace(/-/g, ' ').trim();
  return words ? words.replace(/\b\w/g, (c) => c.toUpperCase()) : id;
}
