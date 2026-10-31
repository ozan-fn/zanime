import { describe, expect, it } from 'vitest'
import { selectFirstSub, setPlayerSource } from './ctl'

// el palsu: source() mencatat argumen; player mencatat track aktif/select.
function fakeEl(opts: { ready?: boolean; active?: boolean } = {}) {
  const sourced: any[] = []
  const selected: Array<string | null> = []
  const player = opts.ready
    ? {
        getSubtitleLangs: () => [{ lang: 'en', label: 'English', active: !!opts.active }],
      }
    : null
  const el: any = {
    player,
    source: (v: any) => sourced.push(v),
    selectSubtitleLang: (l: string | null) => {
      selected.push(l)
      return Promise.resolve(true)
    },
  }
  return { el, sourced, selected, player }
}

const subs = [
  { label: 'English', lang: 'en', url: '/s/sub-en' },
  { label: 'Indonesia', lang: 'id', url: '/s/sub-id' },
]

describe('setPlayerSource', () => {
  it('mendaftarkan subtitle lewat source() bersamaan dengan src', () => {
    const { el, sourced } = fakeEl()
    setPlayerSource(el, '/s/main.m3u8', subs)
    expect(sourced).toEqual([
      {
        video: { src: '/s/main.m3u8' },
        subtitles: [
          { src: '/s/sub-en', lang: 'en', label: 'English', format: 'vtt' },
          { src: '/s/sub-id', lang: 'id', label: 'Indonesia', format: 'vtt' },
        ],
      },
    ])
  })

  it('label kosong jatuh ke lang; tanpa subtitle mendaftarkan daftar kosong', () => {
    const { el, sourced } = fakeEl()
    setPlayerSource(el, '/s/a.mp4', [{ label: '', lang: 'id', url: '/s/sub-id' }])
    setPlayerSource(el, '/s/b.mp4')
    expect(sourced[0].subtitles).toEqual([{ src: '/s/sub-id', lang: 'id', label: 'id', format: 'vtt' }])
    expect(sourced[1].subtitles).toEqual([])
  })
})

describe('selectFirstSub', () => {
  it('mengaktifkan track pertama saat player baru sudah siap', () => {
    const { el, selected } = fakeEl({ ready: true })
    expect(selectFirstSub(el, subs)).toBe(true)
    expect(selected).toEqual(['en'])
  })

  it('tidak menimpa subtitle yang sudah aktif (pilihan tersimpan)', () => {
    const { el, selected } = fakeEl({ ready: true, active: true })
    expect(selectFirstSub(el, subs)).toBe(true)
    expect(selected).toEqual([])
  })

  it('belum siap, instance lama, atau tanpa subtitle → false', () => {
    const { el } = fakeEl()
    const ready = fakeEl({ ready: true })
    expect(selectFirstSub(el, subs)).toBe(false)
    expect(selectFirstSub(ready.el, subs, ready.player)).toBe(false)
    expect(selectFirstSub(ready.el, [])).toBe(false)
    expect(selectFirstSub(null, subs)).toBe(false)
  })
})
