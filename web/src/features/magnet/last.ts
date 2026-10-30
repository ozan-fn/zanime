// Magnet + episode terakhir bertahan setelah reload; kunci localStorage
// dipakai bersama fitur files (thumbnail).

const LAST_KEY = 'zanime.last'

export { LAST_KEY }

export const lastSaved = (): Record<string, any> => {
  try { return JSON.parse(localStorage.getItem(LAST_KEY) ?? '{}') || {} } catch { return {} }
}
export const loadLastSaved = lastSaved

export const saveLast = (patch: Record<string, any>) =>
  localStorage.setItem(LAST_KEY, JSON.stringify({ ...lastSaved(), ...patch }))
