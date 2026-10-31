import { describe, expect, it } from 'vitest'
import { proxied } from '@/lib/api'

describe('proxied', () => {
  it('membungkus URL lewat proxy /fetch dengan query ter-escape', () => {
    expect(proxied('https://host/a.m3u8?x=1 2')).toBe(
      `/fetch?url=${encodeURIComponent('https://host/a.m3u8?x=1 2')}`,
    )
  })
})
