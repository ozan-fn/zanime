// Katalog judul: animeId -> nama. Pencarian mengisi lebih dulu supaya halaman
// tonton punya heading sebelum request episodennya pulang.

const titles = new Map<string, string>();

export function rememberTitles(list: { id: string; name: string }[]) {
  for (const a of list) titles.set(a.id, a.name);
}

export function knownTitle(id: string): string | undefined {
  return titles.get(id);
}
