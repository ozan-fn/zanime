import { api } from '../../lib/api';
import type { SubtitleStatus } from '../../lib/types';

// Semua URL media berada di bawah /api/ agar proxy dev cukup satu entri dan
// Go satu subtree. Tidak ada parameter audio: alurnya sama dengan index.js,
// yang selalu memutar server dub, jadi kualitas saja yang ada di path.
export const masterUrl = (epId: string) => `/api/hls/${encodeURIComponent(epId)}/master.m3u8`;

export const subtitleFileUrl = (epId: string) =>
  `/api/subtitle/${encodeURIComponent(epId)}?lang=id`;

export const subtitleStatus = (epId: string) =>
  api<SubtitleStatus>(`/subtitle/${encodeURIComponent(epId)}/status?lang=id`);
