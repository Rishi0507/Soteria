import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

function render() {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

// Mocks are opt-in and dev-only. The import is dynamic so MSW and the fixture
// data never enter a production bundle, and the import.meta.env.DEV guard means
// setting the flag in a prod build does nothing.
if (import.meta.env.DEV && import.meta.env.VITE_USE_MOCKS === 'true') {
  const { worker } = await import('./mocks/browser')
  await worker.start({ onUnhandledRequest: 'bypass' })
  render()
} else {
  render()
}
