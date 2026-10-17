/** Utilitas VTT: geser waktu cue (delay subtitle). */

/** Detik dari "HH:MM:SS.mmm" atau "MM:SS.mmm". Gagal parse → 0. */
function sec(s: string): number {
  const p = s.trim().split(':').map(Number);
  if (p.some((n) => !Number.isFinite(n))) return 0;
  const [h, m, t] = p.length === 3 ? p : [0, p[0] ?? 0, p[1] ?? 0];
  return h * 3600 + m * 60 + t;
}

/** "HH:MM:SS.mmm" (selalu 3 desimal, seperti VTT). */
function fmt(t: number): string {
  const v = Math.max(0, t);
  const h = Math.floor(v / 3600);
  const m = Math.floor((v % 3600) / 60);
  const s = v % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${s.toFixed(3).padStart(6, '0')}`;
}

/** Satu baris cue. Tanda `-->` bisa lebih dari satu di teks, jadi potong di yang pertama.
 *  Cue setting di belakang timestamp (align:, position:) diteruskan apa adanya. */
function cue(l: string, off: number): string {
  const i = l.indexOf('-->');
  const left = l.slice(0, i);
  const right = l.slice(i + 3).trimStart();
  const sp = right.indexOf(' ');
  const end = sp < 0 ? right : right.slice(0, sp);
  const tail = sp < 0 ? '' : right.slice(sp);
  const a = sec(left) + off;
  const b = sec(end) + off;
  return `${fmt(a)} --> ${fmt(Math.max(a, b))}${tail}`;
}

/** Geser semua timestamp VTT sebesar `off` detik (positif = subtitle muncul lebih lambat).
 *  `off` 0 → teks dikembalikan apa adanya. */
export function shiftVtt(text: string, off: number): string {
  if (!off) return text;
  return text
    .split('\n')
    .map((l) => (l.includes('-->') ? cue(l, off) : l))
    .join('\n');
}
