export interface Anime {
  id: string;
  anilistId: number;
  titleRomaji: string;
  titleEnglish: string | null;
  episodeCount: number | null;
  cover: string;
  banner: string;
  format?: string | null;
  seasonYear?: number | null;
  averageScore?: number | null;
}

export function title(a: Anime): string {
  return a.titleEnglish?.trim() ? a.titleEnglish : a.titleRomaji;
}
