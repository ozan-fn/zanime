import { useEffect } from 'react'
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { clearLogs, log, useLogs } from './logs'

/* Regresi: pola yang dipakai Player — komponen berlangganan store log lalu
 * mencatat dari sebuah effect yang dipasang ulang tiap callback berganti.
 * Dulu setiap render memicu satu baris log baru → render lagi tanpa henti
 * (main thread habis, video membeku). `push` yang tidak berubah harus memutus
 * rantai itu. */

afterEach(() => {
  cleanup()
  clearLogs()
})

let renders = 0

function Child({ onReady }: { onReady: () => void }) {
  useEffect(() => {
    onReady()
  }, [onReady])
  return null
}

function Parent() {
  renders++
  useLogs() // re-render tiap store berubah, seperti LogPanel
  // Sengaja inline: identitasnya berubah tiap render.
  const onReady = () => log.info('siap', 'player:ready')
  return <Child onReady={onReady} />
}

describe('log store loop guard', () => {
  it('baris identik yang berulang tidak memicu render tanpa henti', () => {
    renders = 0
    render(<Parent />)
    // Tanpa penjaga, render berlanjut selamanya (test timeout). Batas longgar
    // supaya tidak rapuh: cukup membuktikan rantainya berhenti.
    expect(renders).toBeLessThan(10)
  })
})
