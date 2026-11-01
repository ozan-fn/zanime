export function Footer() {
  return (
    <footer className="mt-4 flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-3 text-[11px] text-zinc-600 sm:mt-6 sm:pt-4">
      <span>kafuu · streaming film &amp; seri</span>
      <span className="hidden sm:inline">
        metadata <code className="text-zinc-500">./tmp/tmdb</code> · stream{' '}
        <code className="text-zinc-500">./tmp/em</code>
      </span>
    </footer>
  )
}
