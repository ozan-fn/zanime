import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useApp, AppProvider } from '@/features/app/AppProvider'

describe('AppProvider', () => {
  it('play memasang stream aktif, stop melepas, status bisa di-set/clear', () => {
    const { result } = renderHook(() => useApp(), { wrapper: AppProvider })

    act(() => result.current.play({ src: '/fetch?url=https%3A%2F%2Fhost%2Fa.m3u8', title: 'Spider-Man' }))
    expect(result.current.playing?.src).toBe('/fetch?url=https%3A%2F%2Fhost%2Fa.m3u8')
    expect(result.current.playing?.title).toBe('Spider-Man')

    act(() => result.current.setStatus('Memutar…', 'ok'))
    expect(result.current.status?.kind).toBe('ok')

    act(() => result.current.clearStatus())
    expect(result.current.status).toBeNull()

    act(() => result.current.stop())
    expect(result.current.playing).toBeNull()
  })

  it('play dengan src kosong diabaikan', () => {
    const { result } = renderHook(() => useApp(), { wrapper: AppProvider })
    act(() => result.current.play({ src: '' }))
    expect(result.current.playing).toBeNull()
  })

  it('ganti server tanpa subtitle mempertahankan subtitle stream sebelumnya', () => {
    const { result } = renderHook(() => useApp(), { wrapper: AppProvider })
    const media = { tab: 'movie' as const, id: 'tt1' }
    const english = { label: 'English', lang: 'en', url: '/s/sub-en.vtt' }

    act(() => result.current.play({ src: '/s/server-a', media, subs: [english] }))
    act(() => result.current.play({ src: '/s/server-b', media, subs: [] }))

    expect(result.current.playing?.src).toBe('/s/server-b')
    expect(result.current.playing?.subs).toEqual([english])
  })

  it('ganti server dengan subtitle baru menggantikan subtitle lama', () => {
    const { result } = renderHook(() => useApp(), { wrapper: AppProvider })
    const media = { tab: 'movie' as const, id: 'tt1' }
    const english = { label: 'English', lang: 'en', url: '/s/sub-en.vtt' }
    const indonesia = { label: 'Indonesia', lang: 'id', url: '/s/sub-id.vtt' }

    act(() => result.current.play({ src: '/s/server-a', media, subs: [english] }))
    act(() => result.current.play({ src: '/s/server-b', media, subs: [indonesia] }))

    expect(result.current.playing?.subs).toEqual([indonesia])
  })

  it('pindah episode tidak membawa subtitle episode sebelumnya', () => {
    const { result } = renderHook(() => useApp(), { wrapper: AppProvider })
    const english = { label: 'English', lang: 'en', url: '/s/sub-en.vtt' }

    act(() => result.current.play({ src: '/s/ep1', media: { tab: 'tv', id: 'tt1', season: 1, episode: 1 }, subs: [english] }))
    act(() => result.current.play({ src: '/s/ep2', media: { tab: 'tv', id: 'tt1', season: 1, episode: 2 }, subs: [] }))

    expect(result.current.playing?.subs).toEqual([])
  })
})
