import { fetchCatalog, type CatalogParams } from './api';
import { error, loading, sections } from './stores';
import type { HomeSection } from './types';

const now = new Date();
const month = now.getMonth() + 1;
const season = month <= 3 ? 'WINTER' : month <= 6 ? 'SPRING' : month <= 9 ? 'SUMMER' : 'FALL';
const year = now.getFullYear();

const PER_SECTION = 12;

export interface SectionDef {
  key: string;
  title: string;
  params: CatalogParams;
}

/** Definisi section home — semuanya dari satu endpoint /api/catalog. */
export const SECTIONS: SectionDef[] = [
  { key: 'trending', title: 'Sedang Tren', params: { sort: 'TRENDING', limit: PER_SECTION } },
  { key: 'airing', title: 'Sedang Tayang', params: { status: 'RELEASING', sort: 'POPULARITY', limit: PER_SECTION } },
  { key: 'top', title: 'Rating Tertinggi', params: { sort: 'AVERAGE_SCORE', limit: PER_SECTION } },
  { key: 'season', title: `Musim ${season} ${year}`, params: { season, year, sort: 'POPULARITY', limit: PER_SECTION } },
  { key: 'upcoming', title: 'Akan Datang', params: { status: 'NOT_YET_RELEASED', sort: 'NEXT_AIRING_AT', direction: 'ASC', limit: PER_SECTION } },
  { key: 'favorites', title: 'Paling Difavoritkan', params: { sort: 'FAVOURITES', limit: PER_SECTION } },
];

/** Section by key untuk halaman "lihat semua" (`/browse/:key`). */
export function sectionByKey(key: string): SectionDef | undefined {
  return SECTIONS.find((s) => s.key === key);
}

export async function loadHome(): Promise<void> {
  if (SECTIONS.length === 0) return;
  loading.set(true);
  error.set('');
  const results = await Promise.all(
    SECTIONS.map(async ({ key, title, params }) => {
      try {
        return { key, title, items: await fetchCatalog(params) } satisfies HomeSection;
      } catch {
        return { key, title, items: [] } satisfies HomeSection; // satu section gagal ≠ halaman gagal
      }
    }),
  );
  const filled = results.filter((s) => s.items.length > 0);
  if (filled.length === 0) error.set('gagal memuat katalog');
  sections.set(filled);
  loading.set(false);
}
