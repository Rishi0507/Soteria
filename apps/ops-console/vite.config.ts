import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The storefront takes Vite's default 5173. The ops console pins 5174 so the two
// can run side by side, and strictPort makes a clash fail loudly instead of
// silently drifting to another port — the dev origin has to stay predictable
// because it is what the services allow through CORS_ALLOWED_ORIGINS.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5174,
    strictPort: true,
  },
})
