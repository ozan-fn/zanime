import { writable } from 'svelte/store';
import type { AnimeDetail, Episode, Relation } from './types';

export const episodes = writable<Episode[]>([]);
export const detail = writable<AnimeDetail | null>(null);
export const relations = writable<Relation[]>([]);
export const loading = writable(false);
export const error = writable('');
