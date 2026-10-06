import { writable } from 'svelte/store';
import type { Anime } from './types';

export const results = writable<Anime[]>([]);
export const loading = writable(false);
export const error = writable('');
export const lastQuery = writable('');
