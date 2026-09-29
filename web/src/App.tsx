import { Router, Route, useRouter } from 'preact-router';
import { Header } from './components/Header';
import { NotFound } from './components/NotFound';
import { Search } from './features/search/Search';
import { Anime } from './features/watch/Anime';
import { Watch } from './features/watch/Watch';

// Halaman /w/ harus remount penuh ketika episode berganti: shaka memegang
// MSE SourceBuffer dari stream lama di video element, dan sekadar mengganti
// props tidak membongkarnya — player tampak blank. useUrl() berlangganan
// perubahan URL router (useRouter), lalu key dari URL itu memaksa preact
// membongkar & memasang ulang seluruh subtree halaman.
function useUrl(): string {
  const [routerProps] = useRouter();
  return routerProps?.url ?? '';
}

function KeyedPage({ children }: { children: preact.JSX.Element }) {
  return <div key={useUrl()}>{children}</div>;
}

export function KeyedWatch(props: { animeId?: string; epId?: string; mode?: string }) {
  return (
    <KeyedPage>
      <Watch {...props} />
    </KeyedPage>
  );
}

export function KeyedAnime(props: { animeId?: string }) {
  return (
    <KeyedPage>
      <Anime {...props} />
    </KeyedPage>
  );
}

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
          <Route path="/a/:animeId" component={KeyedAnime} />
          <Route path="/w/:animeId/:epId" component={KeyedWatch} />
          <Route default component={NotFound} />
        </Router>
      </main>
    </>
  );
}
