import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AppProvider } from '@/features/app/AppProvider'
import { SearchDialog } from './SearchDialog'
import type { Item, Meta, Server } from '@/lib/types'

/* Kontrak alur pencarian: hasil dari /api/search baris yang tampil,
 * bukan "Masukkan query lalu tekan Cari" — padahal network sudah balas.
 * fetch di-stub; response mengikuti bentuk backend. */

const item = (over: Partial<Item> = {}): Item => ({
  id: 'tt0145487',
  name: 'Spider-Man',
  year: '2002',
  type: 'movie',
  poster: 'https://img/poster.jpg',
  ...over,
})

const meta = (over: Partial<Meta> = {}): Meta => ({
  id: 'tt0145487',
  tmdb: '557',
  name: 'Spider-Man',
  year: '2002',
  ...over,
})

const server = (over: Partial<Server> = {}): Server => ({
  name: 'Premium WMovies',
  type: 'mp4',
  quality: 'FULL HD',
  qualities: [{ label: '1080p', url: 'https://host/1080.mp4' }, { label: '480p', url: 'https://host/480.mp4' }],
  subs: [{ label: 'Indonesia', lang: 'id', url: 'https://sub/id.vtt' }],
  url: 'https://host/1080.mp4',
  ...over,
})

function mount() {
  return render(
    <AppProvider>
      <SearchDialog open onClose={() => {}} />
    </AppProvider>,
  )
}

function stubFetch(search: Item[], metaJSON: Meta = meta(), servers: Server[] = [server()]) {
  const respond = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
  return vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.startsWith('/api/search')) return Promise.resolve(respond({ results: search }))
    if (url.startsWith('/api/meta/')) return Promise.resolve(respond(metaJSON))
    if (url.startsWith('/api/streams')) return Promise.resolve(respond({ servers }))
    return Promise.reject(new Error(`stub tidak tahu URL: ${url}`))
  })
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('SearchDialog', () => {
  it('menampilkan hasil pencarian, bukan empty state, ketika /api/search balas data', async () => {
    vi.stubGlobal('fetch', stubFetch([item()]))
    mount()

    fireEvent.change(screen.getByPlaceholderText(/Cari judul/), { target: { value: 'spiderman' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cari' }))

    await waitFor(() => {
      expect(screen.getByText('Spider-Man')).toBeTruthy()
    })
    expect(screen.queryByText(/Masukkan query lalu tekan Cari/)).toBeNull()
  })

  it('tab Movie: meta mengandung id TMDB langsung membuka daftar stream (per kualitas)', async () => {
    vi.stubGlobal('fetch', stubFetch([item()]))
    mount()

    fireEvent.change(screen.getByPlaceholderText(/Cari judul/), { target: { value: 'spiderman' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cari' }))
    fireEvent.click((await screen.findAllByText('Spider-Man'))[0])

    await waitFor(() => expect(screen.getByText(/1080p/)).toBeTruthy())
    expect(screen.getAllByText(/Premium WMovies/).length).toBeGreaterThan(0)
  })

  it('tab TV: meta membawa episode, lalu memilih episode menuju stream', async () => {
    vi.stubGlobal('fetch', stubFetch(
      [item({ id: 'tt22248376', name: 'Frieren', type: 'tv' })],
      meta({ id: 'tt22248376', name: 'Frieren', tmdb: '209867', episodes: [
        { season: 1, episode: 1, title: 'The Journey to clients', thumbnail: 'https://img/ep.jpg' },
        { season: 1, episode: 2, title: 'Episode 2' },
      ] }),
    ))
    mount()

    fireEvent.click(screen.getByRole('button', { name: 'TV' }))
    fireEvent.change(screen.getByPlaceholderText(/Cari judul/), { target: { value: 'frieren' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cari' }))
    fireEvent.click((await screen.findAllByText('Frieren'))[0])

    await waitFor(() => expect(screen.getByText(/The Journey to clients/)).toBeTruthy())
    expect(screen.getByText('S1')).toBeTruthy()
    fireEvent.click(screen.getByText(/The Journey to clients/))

    // Episode tv sudah minta stream per episode (?s=1&e=).
    await waitFor(() => expect(screen.getAllByText(/Premium WMovies/).length).toBeGreaterThan(0))
    const fetched = vi.mocked(fetch).mock.calls.map(([u]) => String(u))
    expect(fetched.some((u) => u.includes('/api/streams?tab=tv&id=209867&s=1&e=1'))).toBe(true)
  })

  it('server gagal resolve tidak tampil sebagai pilihan stream', async () => {
    vi.stubGlobal('fetch', stubFetch([item()], meta(), [
      server(),
      { name: 'Premium Nepu', type: 'hls', url: 'https://ok/h.m3u8' },
      { name: 'Premium VidStore', type: 'mp4', err: 'server tidak resolve' },
    ]))
    mount()

    fireEvent.change(screen.getByPlaceholderText(/Cari judul/), { target: { value: 'spiderman' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cari' }))
    fireEvent.click((await screen.findAllByText('Spider-Man'))[0])

    await waitFor(() => expect(screen.getByText(/1080p/)).toBeTruthy())
    expect(screen.queryAllByText(/VidStore/).length).toBe(0)
    expect(screen.getAllByText(/Premium Nepu/).length).toBeGreaterThan(0)
  })
})
