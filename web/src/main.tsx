import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AppProvider } from '@/features/app/AppProvider'
import { connectLogs } from '@/lib/logs'
import { HomePage } from '@/pages/HomePage'
import { NotFound } from '@/pages/NotFound'
import './index.css'

// Buka websocket log sekali untuk seluruh aplikasi (bukan di komponen supaya
// test yang me-render komponen tidak ikut menyambung jaringan).
connectLogs()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppProvider>
      <BrowserRouter>
        <Routes>
          <Route index element={<HomePage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </BrowserRouter>
    </AppProvider>
  </StrictMode>,
)
