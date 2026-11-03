import { useWorkers } from '@/lib/logs'

// Angka worker pool aktif di pojok kanan header (tanpa label).
export function WorkerBadge() {
  const workers = useWorkers()
  return (
    <span
      className="grid h-7 min-w-7 place-items-center rounded-lg border border-border bg-card px-1.5 font-mono text-[11px] tabular-nums text-zinc-400"
      title="worker aktif"
      aria-label={`${workers} worker aktif`}
    >
      {workers}
    </span>
  )
}
