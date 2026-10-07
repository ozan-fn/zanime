export interface Episode {
  number: number;
  title: string;
  /** Judul romaji (upstream `x-jat`). */
  titleRomaji?: string;
  /** Judul Jepang (kanji); kosong bila upstream tak punya. */
  titleJp?: string;
  img: string;
}

export interface AnimeDetail {
  malId?: number;
  titleRomaji?: string;
  titleEnglish?: string;
  titles?: { en?: string; ja?: string };
  coverImage?: unknown;
  bannerImage?: unknown;
  backdropUrl?: unknown;
  description?: string;
  episodeCount?: number;
  format?: string;
  status?: string;
  season?: string;
  seasonYear?: number;
  duration?: number;
  averageScore?: number;
  source?: string;
  genres?: string[];
  studios?: string[];
}

/** Gambar upstream bisa string atau object {extraLarge,large,medium,...}. */
export function imgOf(v: unknown): string {
  if (typeof v === 'string') return v;
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>;
    for (const k of ['extraLarge', 'large', 'medium', 'image', 'url', 'src', 'cover', 'banner']) {
      if (typeof o[k] === 'string' && o[k]) return o[k] as string;
    }
  }
  return '';
}

export interface Relation {
  animeId: string;
  anilistId: number;
  title: string;
  image?: unknown;
  coverImage?: unknown;
  episodeCount?: number;
  type?: string;
  seasonYear?: number;
  status?: string;
}

export function detailTitle(d: AnimeDetail): string {
  return d.titleEnglish?.trim() ? d.titleEnglish : (d.titleRomaji ?? '?');
}

/** Judul pendamping di bawah judul utama: romaji lalu kanji (`titles.ja`),
 *  tanpa duplikat dan tanpa yang sama dengan judul utama. */
export function altTitles(d: AnimeDetail): string[] {
  const utama = detailTitle(d);
  const kandidat = [d.titleRomaji?.trim() ?? '', d.titles?.ja?.trim() ?? ''];
  return kandidat.filter((t, i) => t && t !== utama && kandidat.indexOf(t) === i);
}
