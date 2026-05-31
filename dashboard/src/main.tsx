import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import './index.css'
// Lattice design system — "Cool Fabric, Warm Life" tokens, fonts, and chrome
// classes. Imported AFTER index.css so its base (near-true-black bg, Hanken/Plex
// fonts, glow scrollbars, warm selection) wins over the old Tailwind base.
import './design/app.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
