import { http, HttpResponse } from 'msw'

// Origins match the `servers:` block of each OpenAPI spec in contracts/openapi.
const CONTAINMENT = 'http://localhost:8082'
const AUDIT = 'http://localhost:8084'

/**
 * The config the mock currently holds. A PUT updates it, so the screen behaves
 * as it would against the real service across repeated saves.
 */
let config = {
    auto_hold_threshold: 0.85,
    sku_scope_threshold: 0.95,
    updated_by: 'ops:dana',
    updated_at: '2026-09-13T09:20:00Z',
}

/**
 * Escape hatches for the paths that cannot be reached with a valid request:
 *   GET  ?simulate=500   the service is up but failing
 *   PUT  actor "boom"    a 500 on save
 * Everything else mirrors containment-service's real validation ladder in
 * services/containment-service/api/api.go, including where it is stricter than
 * the OpenAPI spec.
 */
export const handlers = [
    http.get(`${CONTAINMENT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'containment-service', bus: 'up' })
    ),
    http.get(`${AUDIT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'audit-proof-service', bus: 'up' })
    ),

    http.get(`${CONTAINMENT}/v1/containment/config`, ({ request }) => {
        if (new URL(request.url).searchParams.get('simulate') === '500') {
            return new HttpResponse(null, { status: 500 })
        }
        return HttpResponse.json(config)
    }),

    http.put(`${CONTAINMENT}/v1/containment/config`, async ({ request }) => {
        const body = (await request.json().catch(() => null)) as {
            auto_hold_threshold?: number
            sku_scope_threshold?: number
            actor?: string
        } | null

        if (!body) {
            return HttpResponse.json(
                { code: 'invalid_body', message: 'malformed JSON' },
                { status: 400 }
            )
        }

        if (body.actor === 'boom') {
            return new HttpResponse(null, { status: 500 })
        }

        // Order matches the real handler: actor first, then each threshold.
        if (!body.actor) {
            return HttpResponse.json(
                { code: 'invalid_body', message: 'actor is required' },
                { status: 400 }
            )
        }

        const auto = body.auto_hold_threshold
        if (typeof auto !== 'number' || auto <= 0 || auto > 1) {
            return HttpResponse.json(
                { code: 'invalid_threshold', message: 'auto_hold_threshold must be in (0, 1]' },
                { status: 400 }
            )
        }

        const sku = body.sku_scope_threshold
        if (typeof sku !== 'number' || sku < 0 || sku > 1) {
            return HttpResponse.json(
                { code: 'invalid_threshold', message: 'sku_scope_threshold must be in [0, 1]' },
                { status: 400 }
            )
        }

        // The service treats 0 as "not supplied" and keeps the stored value —
        // a 200 that changed nothing. Reproduced so the UI is exercised against
        // the real behaviour, not the documented one. The screen does not offer
        // 0, so reaching this needs a hand-made request.
        config = {
            auto_hold_threshold: auto,
            sku_scope_threshold: sku === 0 ? config.sku_scope_threshold : sku,
            updated_by: body.actor,
            updated_at: new Date().toISOString(),
        }
        return HttpResponse.json(config)
    }),
]
