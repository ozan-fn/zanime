import { useEffect, useRef } from 'react'

/* Sq: teks satu baris terpotong; saat hover, teks meluncur ke kiri
 * memperlihatkan bagian terpotongnya (marquee alternating) dan teks
 * lengkapnya jadi tooltip title=. Versi React dari helper setSq(). */
export function Sq({ text, className = '' }: { text: string; className?: string }) {
  const outer = useRef<HTMLSpanElement>(null)
  const inner = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const el = outer.current
    const q = inner.current
    if (!el || !q) return
    const over = q.scrollWidth - el.clientWidth
    if (over > 4) {
      el.style.setProperty('--sq-shift', `-${over}px`)
      el.style.setProperty('--sq-dur', `${Math.max(2, Math.round(over / 30))}s`)
    } else {
      el.style.removeProperty('--sq-shift')
      el.style.removeProperty('--sq-dur')
    }
  }, [text])

  // Marquee hanya saat hover (varian `group-hover` Tailwind sudah dibatasi
  // `@media (hover: hover)`, jadi perangkat sentuh tidak beranimasi).
  return (
    <span
      ref={outer}
      className={`group block min-w-0 overflow-hidden whitespace-nowrap text-ellipsis hover:text-clip ${className}`}
      title={text}
    >
      <span ref={inner} className="inline-block whitespace-nowrap group-hover:animate-sq-slide">{text}</span>
    </span>
  )
}
