import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { AppProvider, useApp } from '@/features/app/AppProvider'
import { ServerPanel } from './ServerPanel'
import type { Streams } from '@/features/search/BrowsePanel'

const streams: Streams = {
  title: 'Spider-Man',
  rows: [
    { name: 'Premium WMovies', type: 'mp4', quality: '1080p', subs: [], url: '/s/1080.mp4' },
    { name: 'Premium Nepu', type: 'hls', quality: '480p', subs: [], url: '/s/480.m3u8' },
  ],
}

// Probe membaca state global supaya klik baris server terbukti memutar stream.
function Now() {
  const { playing } = useApp()
  return <p data-testid="now">{playing?.src || '-'}</p>
}

function mount() {
  return render(
    <AppProvider>
      <ServerPanel streams={streams} />
      <Now />
    </AppProvider>,
  )
}

afterEach(cleanup)

describe('ServerPanel', () => {
  it('klik baris server memutar stream judul yang sedang dibuka', () => {
    mount()
    fireEvent.click(screen.getByRole('button', { name: /1080p/ }))
    expect(screen.getByTestId('now').textContent).toBe('/s/1080.mp4')
  })

  it('bisa di-collapse lalu dibuka lagi; sidebar video tidak terpengaruh', () => {
    mount()
    expect(screen.getByText('Premium WMovies')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Tutup daftar server' }))
    expect(screen.queryByText('Premium WMovies')).toBeNull()
    expect(screen.queryByText('Server')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Buka daftar server' }))
    expect(screen.getByText('Premium WMovies')).toBeTruthy()
  })

  it('terbuka sendiri begitu daftar server datang', () => {
    const view = render(
      <AppProvider>
        <ServerPanel streams={null} />
        <Now />
      </AppProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Tutup daftar server' }))
    expect(screen.queryByText('Server')).toBeNull()

    view.rerender(
      <AppProvider>
        <ServerPanel streams={streams} />
        <Now />
      </AppProvider>,
    )
    expect(screen.getByText('Premium WMovies')).toBeTruthy()
  })

  it('tanpa pilihan judul menampilkan petunjuk, bukan daftar kosong', () => {
    render(
      <AppProvider>
        <ServerPanel streams={null} />
      </AppProvider>,
    )
    expect(screen.getByText(/Pilih judul untuk melihat server/)).toBeTruthy()
  })
})
