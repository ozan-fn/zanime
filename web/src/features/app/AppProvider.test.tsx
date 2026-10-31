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
})
