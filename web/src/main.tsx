import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { applyTheme, loadTheme } from './lib/theme'
import './styles.css'

// Apply the saved theme before the first React paint. The provider applies it
// again on mount, but doing it here avoids a light flash on a dark OS.
applyTheme(loadTheme())

const container = document.getElementById('root')
if (container === null) {
  // Failing loudly beats rendering nothing and wondering why.
  throw new Error('splash: #root is missing from index.html')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
