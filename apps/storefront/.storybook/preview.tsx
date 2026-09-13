import type { Preview } from '@storybook/react-vite'
import { setupWorker } from 'msw/browser'
import { handlers } from '../src/mocks/handlers'
import '../src/index.css'

const worker = setupWorker(...handlers)
const started = worker.start({ onUnhandledRequest: 'bypass' })

const preview: Preview = {
  loaders: [async () => { await started; return {} }],
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
};

export default preview;