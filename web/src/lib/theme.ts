import { writable } from 'svelte/store';

function initial(): boolean {
  try {
    const s = localStorage.getItem('theme');
    if (s) return s === 'dark';
  } catch {
    /* abaikan */
  }
  return true; // default dark
}

/** true = dark. Sinkron ke <html class="dark"> + localStorage. */
export const dark = writable(initial());

dark.subscribe((d) => {
  document.documentElement.classList.toggle('dark', d);
  try {
    localStorage.setItem('theme', d ? 'dark' : 'light');
  } catch {
    /* abaikan */
  }
});
