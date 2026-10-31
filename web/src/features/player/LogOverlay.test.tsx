import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { LogOverlay } from './LogOverlay'
import { clearLogs, log } from '@/lib/logs'

afterEach(() => {
  cleanup()
  clearLogs()
})

describe('LogOverlay', () => {
  it('menampilkan baris dengan badge status per level', () => {
    log.info('uji Premium X…', 'a')
    log.ok('Premium X siap diputar', 'a')
    log.err('Premium Y gagal', 'b')
    render(<LogOverlay />)

    // Baris ber-id sama sudah berganti jadi OK, bukan menumpuk.
    expect(screen.queryByText('uji Premium X…')).toBeNull()
    expect(screen.getByText('Premium X siap diputar')).toBeTruthy()
    expect(screen.getByText('[OK]')).toBeTruthy()
    expect(screen.getByText('Premium Y gagal')).toBeTruthy()
    expect(screen.getByText('[GAGAL]')).toBeTruthy()
  })

  it('menampilkan info "[sedang]" untuk baris berjalan', () => {
    log.info('resolve Premium X…', 'srv:X:resolve')
    render(<LogOverlay />)
    expect(screen.getByText('[sedang]')).toBeTruthy()
    expect(screen.getByText('resolve Premium X…')).toBeTruthy()
  })

  it('kosong → pesan placeholder', () => {
    render(<LogOverlay />)
    expect(screen.getByText('belum ada aktivitas…')).toBeTruthy()
  })
})
