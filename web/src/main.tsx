import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { TorrentStateProvider } from '@/features/torrent-state/TorrentStateProvider'
import { Layout } from '@/pages/Layout'
import { WatchPage } from '@/pages/WatchPage'
import { FilesPage } from '@/pages/FilesPage'
import { StatusPage } from '@/pages/StatusPage'
import { NotFound } from '@/pages/NotFound'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <TorrentStateProvider>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<WatchPage />} />
            <Route path="/files" element={<FilesPage />} />
            <Route path="/status" element={<StatusPage />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </TorrentStateProvider>
  </StrictMode>,
)
