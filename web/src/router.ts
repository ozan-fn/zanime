import Anime from './pages/Anime.svelte';
import Home from './pages/Home.svelte';
import NotFound from './pages/NotFound.svelte';
import Watch from './pages/Watch.svelte';

export const routes = {
  '/': Home,
  '/anime/:id': Anime,
  '/watch/:id/:ep': Watch,
  '*': NotFound,
};
