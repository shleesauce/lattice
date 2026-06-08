import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import './index.css'
// Lattice design system — "Cool Fabric, Warm Life". colors_and_type.css defines
// EVERY token (--base/--raised/--green/--border/glows) + loads the local webfonts;
// it MUST load before app.css, which consumes those tokens. Without it every
// var(--x) silently resolved to empty → transparent modals/buttons/borders.
import './design/colors_and_type.css'
// Chrome + component classes. Imported AFTER index.css so its base (near-true-black
// bg, Hanken/Plex fonts, glow scrollbars, warm selection) wins over Tailwind base.
import './design/app.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
