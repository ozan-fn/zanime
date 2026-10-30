import { useState } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { Clapperboard } from 'lucide-react'
import { Footer } from '@/features/home/Footer'
import { Header } from '@/features/home/Header'
import { SearchDialog } from '@/features/search/SearchDialog'

/* Kerangka halaman bersama: header + nav tab (SPA), Outlet, footer, dan
 * dialog pencarian yang bisa dibuka dari halaman mana pun — state torrent
 * tetap hidup karena provider tak pernah unmount antar-route. */
export function Layout() {
  const [searchOpen, setSearchOpen] = useState(false)

  return (
    <div className="mx-auto flex min-h-screen max-w-7xl flex-col px-4 py-6 sm:px-6 lg:px-8">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Link to="/" className="flex items-center gap-3">
            <div className="grid h-10 w-10 place-items-center rounded-lg border border-border bg-card text-zinc-200">
              <Clapperboard className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-lg font-semibold tracking-tight text-white">kafuu</h1>
              <p className="text-xs text-zinc-500">Streaming torrent lewat HLS</p>
            </div>
          </Link>
          <nav className="ml-0 flex flex-wrap gap-1.5 sm:ml-2" aria-label="Utama">
            <NavTab to="/" end>Putar</NavTab>
            <NavTab to="/files">Video</NavTab>
            <NavTab to="/status">Status</NavTab>
          </nav>
        </div>
        <Header onOpenSearch={() => setSearchOpen(true)} />
      </header>

      <Outlet />

      <Footer />
      <SearchDialog open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  )
}

function NavTab({ to, end, children }: { to: string; end?: boolean; children: string }) {
  return (
    <NavLink to={to} end={end} className={({ isActive }) => `chip${isActive ? ' on' : ''}`}>
      {children}
    </NavLink>
  )
}
