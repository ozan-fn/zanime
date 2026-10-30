import { FilesCard } from '@/features/files/FilesCard'

/* Halaman video: daftar file/episode satu halaman penuh (tidak lagi mau
 * digulir di dalam kartu sempit). Pilih → lompat ke halaman Putar. */
export function FilesPage() {
  return <FilesCard page navigable />
}
