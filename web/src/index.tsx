import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router';
import { App } from './App';
import './global.css';

// Docs React Router (declarative): BrowserRouter membungkus aplikasi, lalu
// <Routes>/<Route> memetakan URL ke komponen. Path nyata (bukan hash), jadi
// back/forward/refresh/link langsung semuanya benar.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
