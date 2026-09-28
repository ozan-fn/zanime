import { Route, Router } from 'preact-router';
import { Header } from './components/Header';
import { NotFound } from './components/NotFound';
import { Search } from './features/search/Search';
import { Anime } from './features/watch/Anime';
import { Watch } from './features/watch/Watch';

// URL adalah sumber kebenaran: /s/{q} pencarian, /a/{id} katalog,
// /w/{animeId}/{epId}?mode=dub pemutaran. Back/refresh/link langsung benar.
export function App() {
  return (
    <>
      <Header />
      <main class="mx-auto max-w-7xl px-4 pb-20 pt-6">
        <Router>
          <Route path="/" component={Search} />
          <Route path="/s/:q?" component={Search} />
          <Route path="/a/:animeId" component={Anime} />
          <Route path="/w/:animeId/:epId" component={Watch} />
          <Route default component={NotFound} />
        </Router>
      </main>
    </>
  );
}
