import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearLogs } from '@/lib/logs'
import { SubtitleCard, type SubtitleMedia } from './SubtitleCard'
import type { Subtitle } from '@/lib/types'

const media: SubtitleMedia = {
  tab: 'tv',
  id: 'tt22248376',
  name: 'Frieren',
  year: '2023',
  season: 2,
  episode: 1,
}

const subs: Subtitle[] = [
  {
    id: '3543430',
    name: 'Frieren.Beyond.Journeys.End.S02E01.CR.WEB-DL.English.EN',
    language: 'English',
    lang: 'english',
    downloads: 1769,
    url: '/api/subtitles/file?u=sub-english.zip',
  },
  {
    id: '999',
    name: 'Paket Indonesia',
    language: 'Indonesian',
    lang: 'indonesian',
    downloads: 5,
    url: '/api/subtitles/file?u=sub-indonesian.zip',
  },
]

function stubFetch() {
  return vi.fn(() =>
    Promise.resolve(
      new Response(JSON.stringify({ subtitles: subs }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  clearLogs()
})

describe('SubtitleCard', () => {
  it('mencari subtitle episode aktif dan memasang yang diklik', async () => {
    vi.stubGlobal('fetch', stubFetch())
    const onUse = vi.fn()
    render(<SubtitleCard media={media} onUse={onUse} />)

    await screen.findByText(/S02E01/)
    const fetched = vi.mocked(fetch).mock.calls.map(([u]) => String(u))
    expect(fetched.some((u) => u.includes('/api/subtitles?tab=tv&id=tt22248376&s=2&e=1'))).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: /Pakai subtitle Paket Indonesia/ }))
    expect(onUse).toHaveBeenCalledWith('/api/subtitles/file?u=sub-indonesian.zip', 'Indonesian')
  })

  it('tombol cari ulang mengambil daftar baru (fresh), bukan memakai hasil lama', async () => {
    vi.stubGlobal('fetch', stubFetch())
    render(<SubtitleCard media={{ ...media, tab: 'movie', season: undefined, episode: undefined }} onUse={vi.fn()} />)
    await screen.findByText(/S02E01/)

    fireEvent.click(screen.getByRole('button', { name: 'Cari subtitle ulang' }))
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.length).toBe(2))
    const last = vi.mocked(fetch).mock.calls.at(-1)!
    expect(String(last[0])).toContain('/api/subtitles?tab=movie&id=tt22248376')
    expect(String(last[0])).toContain('&refresh=1')
    expect(String(last[0])).not.toContain('&s=')
  })

  it('tidak memasang subtitle subdl otomatis; hanya saat diklik', async () => {
    vi.stubGlobal('fetch', stubFetch())
    const onUse = vi.fn()
    render(<SubtitleCard media={media} onUse={onUse} />)

    await screen.findByText(/S02E01/)
    expect(onUse).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: /Pakai subtitle Paket Indonesia/ }))
    expect(onUse).toHaveBeenCalledTimes(1)
  })

  it('bisa di-collapse dan dibuka lagi tanpa mencari ulang', async () => {
    vi.stubGlobal('fetch', stubFetch())
    render(<SubtitleCard media={media} onUse={vi.fn()} />)
    await screen.findByText(/S02E01/)

    fireEvent.click(screen.getByRole('button', { name: 'Tutup pilihan subtitle' }))
    expect(screen.queryByText(/S02E01/)).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Buka pilihan subtitle' }))
    expect(screen.getByText(/S02E01/)).toBeTruthy()
    expect(vi.mocked(fetch).mock.calls.length).toBe(1)
  })

  it('tanpa judul tidak memanggil backend dan menampilkan petunjuk', () => {
    const fetchStub = vi.fn()
    vi.stubGlobal('fetch', fetchStub)
    render(<SubtitleCard onUse={vi.fn()} />)

    expect(screen.getByText(/Pilih judul dulu/)).toBeTruthy()
    expect(fetchStub).not.toHaveBeenCalled()
  })
})
