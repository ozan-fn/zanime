import { Link } from 'react-router-dom'

export function NotFound() {
  return (
    <div className="grid min-h-screen place-items-center px-4 text-zinc-400">
      <div className="text-center">
        <p className="text-4xl font-semibold text-zinc-200">404</p>
        <p className="mt-2 text-sm">Halaman tidak ada.</p>
        <Link to="/" className="mt-4 inline-block rounded-lg border border-border bg-card px-4 py-2 text-sm text-zinc-300 transition hover:bg-zinc-800 hover:text-white">
          Kembali ke kafuu
        </Link>
      </div>
    </div>
  )
}
