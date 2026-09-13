import { http, HttpResponse } from 'msw'
import type { ContainmentAction } from '../api/containment'
import { queue } from './fixtures'
import type { Dossier } from '../api/audit'
import {
    archiveRows,
    dossierComplete,
    dossierWithNote,
    verifiedBroken,
    verifiedOK,
} from './auditFixtures'

// Origins match the `servers:` block of each OpenAPI spec in contracts/openapi.
const CONTAINMENT = 'http://localhost:8082'
const AUDIT = 'http://localhost:8085'

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

/** Dossiers the mock currently holds, keyed by incident. */
let dossiers: Record<string, Dossier> = {
    [dossierComplete.incident_id]: dossierComplete,
    [dossierWithNote.incident_id]: dossierWithNote,
}
let archive = archiveRows.map((r) => ({ ...r }))

/** Mutable copy so a confirm or reject is visible on the next read. */
let actions: ContainmentAction[] = queue.map((a) => ({ ...a }))

/**
 * Escape hatches for paths a valid request cannot reach:
 *   GET  /config            ?simulate=500
 *   PUT  /config            actor "boom"
 *   GET  /actions           ?simulate=500, ?simulate=empty
 *   POST /confirm, /reject  actor "boom"  -> 500
 *                           actor "taken" -> 409 (another reviewer got there first)
 * Everything else mirrors containment-service's real behaviour, including where
 * it is looser or stricter than the OpenAPI spec.
 */
export const handlers = [
    http.get(`${CONTAINMENT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'containment-service', bus: 'up' })
    ),
    http.get(`${AUDIT}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'audit-proof-service', bus: 'up' })
    ),

    // ------------------------------------------------------------ config

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

        config = {
            auto_hold_threshold: auto,
            sku_scope_threshold: sku === 0 ? config.sku_scope_threshold : sku,
            updated_by: body.actor,
            updated_at: new Date().toISOString(),
        }
        return HttpResponse.json(config)
    }),

    // ------------------------------------------------------ review queue

    http.get(`${CONTAINMENT}/v1/containment/actions`, ({ request }) => {
        const params = new URL(request.url).searchParams
        const simulate = params.get('simulate')
        if (simulate === '500') return new HttpResponse(null, { status: 500 })
        if (simulate === 'empty') return HttpResponse.json({ items: [] })

        const status = params.get('status')
        // The real Store.List does a raw string compare: an unrecognised status
        // matches nothing rather than being rejected.
        const items = status ? actions.filter((a) => a.status === status) : actions
        return HttpResponse.json({ items })
    }),

    http.get(`${CONTAINMENT}/v1/containment/actions/:actionId`, ({ params }) => {
        const found = actions.find((a) => a.action_id === params.actionId)
        if (!found) {
            return HttpResponse.json(
                { code: 'not_found', message: 'no such containment action' },
                { status: 404 }
            )
        }
        return HttpResponse.json(found)
    }),

    http.post(
        `${CONTAINMENT}/v1/containment/actions/:actionId/confirm`,
        async ({ params, request }) => {
            const body = (await request.json().catch(() => null)) as {
                actor?: string
                note?: string
                lot_codes_override?: string[]
            } | null

            const found = actions.find((a) => a.action_id === params.actionId)
            if (!found) {
                return HttpResponse.json(
                    { code: 'not_found', message: 'no such containment action' },
                    { status: 404 }
                )
            }
            if (body?.actor === 'boom') return new HttpResponse(null, { status: 500 })
            if (body?.actor === 'taken') {
                // Another reviewer decided first: the service sees a non-pending
                // status and answers 409.
                const decided: ContainmentAction = {
                    ...found,
                    status: 'HUMAN_CONFIRMED',
                    actor: 'ops:sam',
                    decided_at: new Date().toISOString(),
                    results: found.targets.map((t) => ({
                        gtin: t.gtin,
                        status: 'HELD' as const,
                        platform: 'shopify',
                        units_held: 25,
                        units_left_sellable: 75,
                    })),
                }
                actions = actions.map((a) => (a.action_id === found.action_id ? decided : a))
                return HttpResponse.json(
                    { code: 'conflict', message: 'action is not pending review' },
                    { status: 409 }
                )
            }
            if (!body || !body.actor) {
                return HttpResponse.json(
                    { code: 'invalid_body', message: 'actor is required' },
                    { status: 400 }
                )
            }
            if (found.status !== 'PENDING_REVIEW') {
                return HttpResponse.json(
                    { code: 'conflict', message: 'action is not pending review' },
                    { status: 409 }
                )
            }

            // narrowLots: intersect per target; an empty intersection leaves the
            // target's lots untouched, with no error and no signal.
            const override = body.lot_codes_override ?? []
            const allow = new Set(override.map((c) => c.trim().toUpperCase()))
            const targets = found.targets.map((t) => {
                if (override.length === 0) return t
                const kept = (t.lot_codes ?? []).filter((l) =>
                    allow.has(l.trim().toUpperCase())
                )
                return kept.length > 0 ? { ...t, lot_codes: kept, scope: 'LOT' as const } : t
            })

            // One target fails on the multi-target fixture, so the partial-failure
            // path is reachable live rather than only in a story.
            const results = targets.map((t, i) =>
                targets.length > 1 && i === targets.length - 1
                    ? {
                          gtin: t.gtin,
                          status: 'FAILED' as const,
                          platform: 'shopify',
                          error: 'inventoryMoveQuantities: location Quarantine not found',
                      }
                    : {
                          gtin: t.gtin,
                          status: 'HELD' as const,
                          platform: 'shopify',
                          platform_ref: `gid://shopify/InventoryItem/${44120 + i}`,
                          units_held: 20 * (t.lot_codes?.length ?? 1),
                          units_left_sellable: 60,
                      }
            )

            const updated: ContainmentAction = {
                ...found,
                targets,
                status: 'HUMAN_CONFIRMED',
                actor: body.actor,
                note: body.note,
                decided_at: new Date().toISOString(),
                results,
            }
            actions = actions.map((a) => (a.action_id === found.action_id ? updated : a))
            return HttpResponse.json(updated)
        }
    ),

    http.post(
        `${CONTAINMENT}/v1/containment/actions/:actionId/reject`,
        async ({ params, request }) => {
            const body = (await request.json().catch(() => null)) as {
                actor?: string
                reason?: string
            } | null

            const found = actions.find((a) => a.action_id === params.actionId)
            if (!found) {
                return HttpResponse.json(
                    { code: 'not_found', message: 'no such containment action' },
                    { status: 404 }
                )
            }
            if (body?.actor === 'boom') return new HttpResponse(null, { status: 500 })
            if (!body || !body.actor || !body.reason) {
                return HttpResponse.json(
                    { code: 'invalid_body', message: 'actor and reason are required' },
                    { status: 400 }
                )
            }
            if (found.status !== 'PENDING_REVIEW') {
                return HttpResponse.json(
                    { code: 'conflict', message: 'action is not pending review' },
                    { status: 409 }
                )
            }

            const updated: ContainmentAction = {
                ...found,
                status: 'HUMAN_REJECTED',
                actor: body.actor,
                // The service stores the reviewer's reason in `note` and leaves
                // `reason` holding the queueing explanation.
                note: body.reason,
                decided_at: new Date().toISOString(),
                results: [],
            }
            actions = actions.map((a) => (a.action_id === found.action_id ? updated : a))
            return HttpResponse.json(updated)
        }
    ),

    // ------------------------------------------------------ dossier archive

    http.get(`${AUDIT}/v1/dossiers`, ({ request }) => {
        const simulate = new URL(request.url).searchParams.get('simulate')
        if (simulate === '500') return new HttpResponse(null, { status: 500 })
        // An emptied ledger and a quiet period are the same response.
        if (simulate === 'empty') return HttpResponse.json({ items: [] })
        return HttpResponse.json({ items: archive })
    }),

    http.get(`${AUDIT}/v1/dossiers/:incidentId`, ({ params }) => {
        const raw = String(params.incidentId)
        // The handler parses `.pdf` off the path segment itself, case-sensitively.
        if (raw.endsWith('.pdf')) {
            const incident = raw.slice(0, -4)
            if (!dossiers[incident]) {
                return HttpResponse.json(
                    { code: 'not_found', message: 'no dossier has been generated for this incident' },
                    { status: 404 }
                )
            }
            return new HttpResponse('%PDF-1.4 mock dossier', {
                headers: {
                    'Content-Type': 'application/pdf',
                    'Content-Disposition': `inline; filename="soteria-dossier-${incident}.pdf"`,
                },
            })
        }

        const found = dossiers[raw]
        if (!found) {
            // Ledger-derived list, dossier-derived detail: this is the ordinary
            // "not generated yet" case, not a fault.
            return HttpResponse.json(
                { code: 'not_found', message: 'no dossier has been generated for this incident' },
                { status: 404 }
            )
        }
        return HttpResponse.json(found)
    }),

    http.post(`${AUDIT}/v1/dossiers/:incidentId`, ({ params }) => {
        const incident = String(params.incidentId)
        const row = archive.find((r) => r.incident_id === incident)
        if (!row) {
            return HttpResponse.json(
                { code: 'not_found', message: `audit: no events recorded for incident "${incident}"` },
                { status: 404 }
            )
        }
        // Not idempotent: a new id and a new generated_at every single time.
        const base = dossiers[incident] ?? dossierWithNote
        const fresh: Dossier = {
            ...base,
            incident_id: incident,
            dossier_id: crypto.randomUUID(),
            generated_at: new Date().toISOString(),
            content_hash: row.content_hash ?? base.content_hash,
            event_count: row.event_count,
        }
        dossiers = { ...dossiers, [incident]: fresh }
        archive = archive.map((r) =>
            r.incident_id === incident ? { ...r, has_dossier: true } : r
        )
        return HttpResponse.json(fresh, { status: 201 })
    }),

    http.get(`${AUDIT}/v1/dossiers/:incidentId/verify`, ({ params, request }) => {
        const incident = String(params.incidentId)
        const simulate = new URL(request.url).searchParams.get('simulate')
        if (!archive.some((r) => r.incident_id === incident)) {
            return HttpResponse.json(
                { code: 'not_found', message: 'no events recorded for this incident' },
                { status: 404 }
            )
        }
        // A broken chain is a 200 whose body omits content_hash and hash_algorithm.
        if (simulate === 'broken' || incident === verifiedBroken.incident_id) {
            return HttpResponse.json({ ...verifiedBroken, incident_id: incident })
        }
        const row = archive.find((r) => r.incident_id === incident)
        return HttpResponse.json({
            ...verifiedOK,
            incident_id: incident,
            events: row?.event_count ?? verifiedOK.events,
            content_hash: row?.content_hash ?? verifiedOK.content_hash,
        })
    }),

    http.get(`${AUDIT}/v1/dossiers/:incidentId/chain`, ({ params }) => {
        const incident = String(params.incidentId)
        const row = archive.find((r) => r.incident_id === incident)
        if (!row) {
            return HttpResponse.json(
                { code: 'not_found', message: 'no events recorded for this incident' },
                { status: 404 }
            )
        }
        return HttpResponse.json({
            incident_id: incident,
            head: row.content_hash,
            records: [],
        })
    }),
]

/** Restores the queue after manual pokes in dev. */
export function resetQueue() {
    actions = queue.map((a) => ({ ...a }))
}
