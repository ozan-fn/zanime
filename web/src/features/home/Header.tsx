import { Search } from 'lucide-react'

/* Header: satu-satunya aksi adalah membuka dialog pencarian. */
export function Header({ onOpenSearch }: { onOpenSearch: () => void }) {
  return (
    <div className="flex items-center gap-2">
      <button
        type="button"
        onClick={onOpenSearch}
        className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-border bg-card px-3 text-xs font-medium text-zinc-300 transition hover:bg-zinc-800 hover:text-white"
      >
        <Search className="h-3.5 w-3.5" />
        Cari
      </button>
    </div>
  )
}
