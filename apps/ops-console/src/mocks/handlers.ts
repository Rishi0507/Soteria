import { http, HttpResponse } from 'msw'

// Origins match the `servers:` block of each OpenAPI spec in contracts/openapi.
const CONTAINMENT = 'http://localhost:8082'
const AUDIT = 'http://localhost:8084'

// Scaffold only. These two handlers exist so the MSW wiring is provably live
// before any screen is built: with VITE_USE_MOCKS=true the health probes answer
// without a backend. Handlers for the review queue, threshold config and dossier
// archive land with the screens that consume them.
export const handlers = [
    http.get(`${CONTAINMENT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'containment-service', bus: 'up' })
    ),
    http.get(`${AUDIT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'audit-proof-service', bus: 'up' })
    ),
]
