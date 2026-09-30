// URL helpers shared across features.

export const watchHref = (animeId: string, epId: string) =>
  `/w/${encodeURIComponent(animeId)}/${encodeURIComponent(epId)}`;

export const animeHref = (id: string) => `/a/${encodeURIComponent(id)}`;

// safeDecode never throws: a hand-typed URL like /s/100% would make
// decodeURIComponent raise URIError and blank the whole page.
export function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

// slugTitle turns "one-piece-123" into "One Piece" for the heading shown
// before the catalog request returns the real title.
export function slugTitle(id: string) {
  const words = id.replace(/-\d+$/, '').replace(/-/g, ' ').trim();
  return words ? words.replace(/\b\w/g, (c) => c.toUpperCase()) : id;
}
