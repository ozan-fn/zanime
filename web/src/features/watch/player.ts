import { api } from '../../lib/api';
import type { SubtitleStatus } from '../../lib/types';

// Semua URL media berada di bawah /api/ agar proxy dev cukup satu entri dan
// Go satu subtree. Mode naik di query, bukan path: browser membuang query
// saat me-resolve URI segmen relatif, jadi URI varian yang ditulis Go tetap
// benar tanpa perlu tahu query apa pun.
export const masterUrl = (epId: string, mode: 'sub' | 'dub') =>
  `/api/hls/${encodeURIComponent(epId)}/master.m3u8?mode=${mode}`;

export const subtitleFileUrl = (epId: string, mode: 'sub' | 'dub') =>
  `/api/subtitle/${encodeURIComponent(epId)}?mode=${mode}&lang=id`;

export const subtitleStatus = (epId: string, mode: 'sub' | 'dub') =>
  api<SubtitleStatus>(`/subtitle/${encodeURIComponent(epId)}/status?mode=${mode}&lang=id`);
