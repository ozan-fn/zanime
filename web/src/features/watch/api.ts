import { get, post } from '../../lib/api';
import type { Stream } from './types';

export function fetchStream(id: string, ep: number): Promise<Stream> {
  return get<Stream>(`/api/stream?id=${encodeURIComponent(id)}&ep=${ep}`);
}

export interface SkipInterval { startTime: number; endTime: number }
export interface SkipTime { interval: SkipInterval }

export async function fetchSkiptimes(mal: number, ep: number, dur: number): Promise<SkipTime[]> {
  const v = await get<{ results?: Array<{ interval: SkipInterval }> }>(`/api/skiptimes?mal=${mal}&ep=${ep}&len=${dur}`);
  return v?.results ?? [];
}

export interface SubStatus {
  state: string;
  queue: number;
  queue_total: number;
  eta_sec: number;
  done: number;
  total: number;
  next_at: number;
  error?: string;
  vtt?: string;
}

/** Job terjemahan ID on-demand: batch per 6 cue (tiap 3 tampil / posisi). */
export function postSubBatch(id: string, at?: number): Promise<SubStatus> {
  return post<SubStatus>('/api/subid/batch', { id, at });
}

export function getSubStatus(id: string, at?: number): Promise<SubStatus> {
  return get<SubStatus>(`/api/subid/status?id=${encodeURIComponent(id)}${at != null ? `&at=${at}` : ''}`);
}

export function subResultUrl(id: string): string {
  return `/api/subid/result?id=${encodeURIComponent(id)}&only=id`;
}
