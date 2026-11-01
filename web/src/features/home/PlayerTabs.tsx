import { useState } from 'react'
import { ServerPanel } from '@/features/player/ServerPanel'
import { LogPanel } from '@/features/log/LogPanel'
import { SubtitleCard, type SubtitleMedia } from '@/features/subtitle/SubtitleCard'
import type { Streams } from '@/features/search/BrowsePanel'

/* Ponsel/tablet: server, subtitle, dan log berbagi satu area di bawah player.
 * Ketiganya tetap ter-mount (disembunyikan lewat `hidden`), jadi pindah tab
 * tidak memicu pencarian subtitle ulang dan riwayat log tidak hilang. */

const TABS = ['Server', 'Subtitle', 'Log'] as const
type Tab = (typeof TABS)[number]

export function PlayerTabs({
  streams,
  media,
  auto,
  onUse,
}: {
  streams: Streams | null
  media?: SubtitleMedia
  auto?: boolean
  onUse: (src: string, label: string) => void
}) {
  const [tab, setTab] = useState<Tab>('Server')

  return (
    <div className="mt-3">
      <div
        role="tablist"
        aria-label="Panel pemutar"
        className="grid grid-cols-3 gap-1 rounded-lg border border-border bg-card p-1"
      >
        {TABS.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`rounded px-2 py-1.5 text-[11px] transition ${
              tab === t
                ? 'bg-primary font-medium text-primary-contrast'
                : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      <div className="mt-2">
        <div className={tab === 'Server' ? '' : 'hidden'}>
          <ServerPanel streams={streams} />
        </div>
        <div className={tab === 'Subtitle' ? '' : 'hidden'}>
          <SubtitleCard media={media} auto={auto} onUse={onUse} />
        </div>
        <div className={tab === 'Log' ? '' : 'hidden'}>
          <LogPanel className="rounded-lg border border-border" panelClassName="rounded-lg" maxHeight="max-h-64" />
        </div>
      </div>
    </div>
  )
}
