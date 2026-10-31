import { describe, expect, it, vi } from 'vitest'
import { attachSubs } from './ctl'

// el palsu: player mencatat track; elemen mencatat pemanggilan menu/select.
function fakeEl(subs: boolean) {
  const added: any[] = []
  const selected: Array<string | null> = []
  const menu = vi.fn()
  const el: any = {
    player: subs
      ? {
          addSubtitleTrack: (e: any) => added.push(e),
        }
      : null,
    updateSubtitleTrackMenu: menu,
    refreshOpenContextMenu: vi.fn(),
    selectSubtitleLang: (l: string | null) => {
      selected.push(l)
      return Promise.resolve(true)
    },
  }
  return { el, added, selected, menu }
}

describe('attachSubs', () => {
  it('mendaftarkan tiap subtitle lewat API player lalu mengaktifkan yang pertama', () => {
    const { el, added, selected, menu } = fakeEl(true)
    const subs = [
      { label: 'English', lang: 'en', url: '/s/sub-en' },
      { label: 'Indonesia', lang: 'id', url: '/s/sub-id' },
    ]

    expect(attachSubs(el, subs)).toBe(true)
    expect(added).toEqual([
      { url: '/s/sub-en', lang: 'en', label: 'English', format: 'vtt' },
      { url: '/s/sub-id', lang: 'id', label: 'Indonesia', format: 'vtt' },
    ])
    expect(selected).toEqual(['en'])
    expect(menu).toHaveBeenCalled()
  })

  it('label kosong jatuh ke lang; satu subtitle tetap jalan', () => {
    const { el, added, selected } = fakeEl(true)
    expect(attachSubs(el, [{ label: '', lang: 'id', url: '/s/sub-id' }])).toBe(true)
    expect(added[0].label).toBe('id')
    expect(selected).toEqual(['id'])
  })

  it('belum siap (player null) atau tanpa subtitle → false, tidak menyentuh apa pun', () => {
    const { el, added, menu } = fakeEl(false)
    const subs = [{ label: 'English', lang: 'en', url: '/s/sub-en' }]

    expect(attachSubs(el, subs)).toBe(false)
    expect(attachSubs(el, [])).toBe(false)
    expect(attachSubs(null, subs)).toBe(false)
    expect(added).toEqual([])
    expect(menu).not.toHaveBeenCalled()
  })
})
