import { writable } from 'svelte/store';
import type { HomeSection } from './types';

export const sections = writable<HomeSection[]>([]);
export const loading = writable(false);
export const error = writable('');
