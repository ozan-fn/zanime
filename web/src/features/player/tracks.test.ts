import { describe, expect, it } from 'vitest'
import { cueCount, subTracks, toVtt, uniqueLabel } from './tracks'

describe('subTracks', () => {
  it('memetakan subtitle aman, track pertama default, label jatuh ke lang', () => {
    expect(
      subTracks([
        { label: 'Indonesia', lang: 'id', url: '/s/sub-id' },
        { label: '', lang: 'en', url: 'https://cdn/sub-en.vtt' },
      ]),
    ).toEqual([
      { src: '/s/sub-id', kind: 'subtitles', label: 'Indonesia', lang: 'id', default: true },
      { src: 'https://cdn/sub-en.vtt', kind: 'subtitles', label: 'en', lang: 'en', default: false },
    ])
  })

  it('membuang URL tidak aman atau kosong, lang kosong, dan duplikat lang', () => {
    expect(
      subTracks([
        { label: 'X', lang: 'id', url: 'javascript:alert(1)' },
        { label: 'Y', lang: 'id', url: 'data:text/vtt,AAAA' },
        { label: 'Z', lang: '', url: '/s/a.vtt' },
        { label: 'A', lang: 'en', url: '' },
        { label: 'B', lang: 'en', url: '/s/en.vtt' },
        { label: 'C', lang: 'en', url: '/s/en2.vtt' },
      ]),
    ).toEqual([{ src: '/s/en.vtt', kind: 'subtitles', label: 'B', lang: 'en', default: true }])
  })

  it('Indonesia dipilih walau bukan track pertama, Inggris jadi fallback', () => {
    const tracks = subTracks([
      { label: 'France', lang: 'fr', url: '/s/fr.vtt' },
      { label: 'English', lang: 'en', url: '/s/en.vtt' },
      { label: 'Indonesian', lang: 'id-ID', url: '/s/id.vtt' },
    ])
    expect(tracks.filter((t) => t.default).map((t) => t.lang)).toEqual(['id-ID'])

    const enOnly = subTracks([
      { label: 'France', lang: 'fr', url: '/s/fr.vtt' },
      { label: 'English', lang: 'en', url: '/s/en.vtt' },
    ])
    expect(enOnly.find((t) => t.default)?.lang).toBe('en')
  })

  it('label dipakai saat lang tidak dikenali; tanpa id/en tidak ada yang default', () => {
    const byLabel = subTracks([{ label: 'Bahasa Indonesia', lang: 'xx', url: '/s/a.vtt' }])
    expect(byLabel[0].default).toBe(true)

    const none = subTracks([{ label: 'Deutsch', lang: 'de', url: '/s/de.vtt' }])
    expect(none[0].default).toBe(false)
  })

  it('tanpa subtitle → daftar kosong', () => {
    expect(subTracks()).toEqual([])
  })
})

describe('uniqueLabel', () => {
  it('label yang belum dipakai dibiarkan, spasi dipangkas', () => {
    expect(uniqueLabel(' Indonesia ', ['English'])).toBe('Indonesia')
  })

  it('label bentrok diberi akhiran angka sampai unik', () => {
    expect(uniqueLabel('English', ['English'])).toBe('English (2)')
    expect(uniqueLabel('English', ['English', 'English (2)'])).toBe('English (3)')
  })

  it('label kosong jatuh ke "Subtitle"', () => {
    expect(uniqueLabel('', [])).toBe('Subtitle')
  })
})

describe('toVtt', () => {
  const srt = '1\r\n00:00:01,500 --> 00:00:04,000\r\nHalo, dunia.\r\n\r\n2\r\n00:00:05,000 --> 00:00:07,250\r\nBaris kedua.\r\n'

  it('SRT diberi header, CRLF dan koma waktu dinormalkan, teks cue utuh', () => {
    expect(toVtt(srt)).toBe(
      'WEBVTT\n\n1\n00:00:01.500 --> 00:00:04.000\nHalo, dunia.\n\n2\n00:00:05.000 --> 00:00:07.250\nBaris kedua.\n',
    )
    expect(cueCount(toVtt(srt))).toBe(2)
  })

  it('VTT dilewatkan apa adanya, BOM dibuang', () => {
    const vtt = 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHai, ini VTT.\n'
    expect(toVtt(`\uFEFF${vtt}`)).toBe(vtt)
  })

  it('teks tanpa cue tidak dihitung', () => {
    expect(cueCount('WEBVTT\n\nHalo\n')).toBe(0)
  })
})
