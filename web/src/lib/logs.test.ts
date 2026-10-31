import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearLogs, getLogs, log, push, pushEvent, subscribeLogs } from './logs'

afterEach(() => clearLogs())

describe('logs store', () => {
  it('push menambah baris dan memberi tahu pelanggan', () => {
    const fn = vi.fn()
    const off = subscribeLogs(fn)
    push('info', 'halo')
    expect(getLogs()).toHaveLength(1)
    expect(getLogs()[0]).toMatchObject({ level: 'info', text: 'halo' })
    expect(fn).toHaveBeenCalledTimes(1)

    off()
    push('ok', 'lagi')
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('baris ber-id sama digantikan di tempat (sedang → OK)', () => {
    log.info('uji Premium X…', 'srv:X')
    log.ok('Premium X siap diputar', 'srv:X')
    expect(getLogs()).toHaveLength(1)
    expect(getLogs()[0]).toMatchObject({ id: 'srv:X', level: 'ok', text: 'Premium X siap diputar' })
  })

  it('pushEvent menormalkan level dan mempertahankan waktu server', () => {
    pushEvent({ id: 'a', at: '12:00:00.000', level: 'ok', text: 'ok' })
    pushEvent({ level: 'ngawur', text: 'x' })
    expect(getLogs()[0]).toMatchObject({ id: 'a', at: '12:00:00.000', level: 'ok', text: 'ok' })
    expect(getLogs()[1].level).toBe('info')
    expect(getLogs()[1].at).toMatch(/^\d{2}:\d{2}:\d{2}\.\d{3}$/)
  })

  it('membatasi jumlah baris (ring buffer)', () => {
    for (let i = 0; i < 450; i++) push('info', `b${i}`)
    expect(getLogs()).toHaveLength(400)
    expect(getLogs().at(-1)!.text).toBe('b449')
    expect(getLogs()[0].text).toBe('b50')
  })

  it('clearLogs mengosongkan', () => {
    push('info', 'x')
    clearLogs()
    expect(getLogs()).toHaveLength(0)
  })
})
