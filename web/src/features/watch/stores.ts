import { writable } from 'svelte/store';
import type { Stream } from './types';

export const stream = writable<Stream | null>(null);
export const loading = writable(false);
export const error = writable('');
