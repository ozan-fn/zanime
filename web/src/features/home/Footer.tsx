import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { fmt } from '@/lib/fmt'

export function Footer() {
  const { state } = useTorrent()
  return (
    <footer className="mt-6 flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-4 text-[11px] text-zinc-600">
      <span>kafuu · streaming torrent</span>
      <span>
        cache <span>{state?.cache ? fmt(state.cache.limit) : '4 GiB'}</span> · data torrent di{' '}
        <code className="text-zinc-500">{state?.cache?.dir || './tmp/cache'}</code>
      </span>
    </footer>
  )
}
