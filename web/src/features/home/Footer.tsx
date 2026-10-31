export function Footer() {
  return (
    <footer className="mt-6 flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-4 text-[11px] text-zinc-600">
      <span>kafuu · streaming film &amp; seri</span>
      <span>
        metadata <code className="text-zinc-500">./tmp/tmdb</code> · stream{' '}
        <code className="text-zinc-500">./tmp/em</code>
      </span>
    </footer>
  )
}
