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
  status?: string | null;
}

export function title(a: Anime): string {
  return a.titleEnglish?.trim() ? a.titleEnglish : a.titleRomaji;
}

/** Status upstream → label pendek buat chip di kartu. */
const STATUS_LABEL: Record<string, string> = {
  RELEASING: 'Tayang',
  FINISHED: 'Tamat',
  NOT_YET_RELEASED: 'Akan datang',
  CANCELLED: 'Dibatalkan',
  HIATUS: 'Hiatus',
};

export function statusLabel(s: string | null | undefined): string {
  return (s && (STATUS_LABEL[s] ?? s)) || '';
}
