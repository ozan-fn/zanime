import { useEffect, type ReactNode } from 'react';
import { Route, Routes, useLocation } from 'react-router';
import { Header } from './components/Header';
import { NotFound } from './components/NotFound';
import { Search } from './features/search/Search';
import { Anime } from './features/watch/Anime';
import { Watch } from './features/watch/Watch';

// Halaman /w/ harus remount penuh ketika episode berganti: player memegang
// stream lama di video element, dan sekadar mengganti props tidak membongkar
// & memasang ulang subtree halaman (docs React: "Resetting state with a key").
// Query tidak ikut jadi kunci: hanya path yang menentukan halaman.
function KeyedPage({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  return <div key={pathname}>{children}</div>;
}

// ScrollToTop mengembalikan posisi gulir tiap URL berubah. Tanpa ini, membuka
// episode dari daftar panjang (atau hasil pencarian dari bawah) mendarat di
// tengah halaman baru karena browser mempertahankan posisi lama.
function ScrollToTop() {
  const { pathname } = useLocation();
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);
  return null;
}

// URL adalah sumber kebenaran: /s/{q} pencarian, /a/{id} katalog,
// /w/{animeId}/{epId} pemutaran. Back/refresh/link langsung benar.
export function App() {
  return (
    <>
      <ScrollToTop />
      <Header />
      <main className="mx-auto max-w-7xl px-4 pb-20 pt-6">
        <Routes>
          <Route path="/" element={<Search />} />
          <Route path="/s/:q?" element={<Search />} />
          <Route
            path="/a/:animeId"
            element={
              <KeyedPage>
                <Anime />
              </KeyedPage>
            }
          />
          <Route
            path="/w/:animeId/:epId"
            element={
              <KeyedPage>
                <Watch />
              </KeyedPage>
            }
          />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
    </>
  );
}
