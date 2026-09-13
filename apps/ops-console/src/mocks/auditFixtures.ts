import type { ArchiveRow, Chain, Dossier, Verification } from '../api/audit'

const HEAD_1 = 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90'
const HEAD_2 = '00ffee11dd22cc33bb44aa5599668877006fee11dd22cc33bb44aa5599668877'
const HEAD_3 = '7788990011aabbccddeeff00112233447788990011aabbccddeeff0011223344'

/**
 * The archive is ledger-derived: a row exists for every incident with recorded
 * events, whether or not a dossier was built. `inc-eu_rasff-2026-0917` is the
 * has_dossier:false case, and getDossier 404s on it.
 */
export const archiveRows: ArchiveRow[] = [
    {
        incident_id: 'inc-fda_enforcement-f-2291-2026',
        has_dossier: true,
        event_count: 7,
        content_hash: HEAD_1,
    },
    {
        incident_id: 'inc-usda_fsis-031-2026',
        has_dossier: true,
        event_count: 5,
        content_hash: HEAD_2,
    },
    {
        incident_id: 'inc-eu_rasff-2026-0917',
        has_dossier: false,
        event_count: 2,
        content_hash: HEAD_3,
    },
]

export const dossierComplete: Dossier = {
    dossier_id: '9f1c8a52-4f2e-4a7b-9b0e-2c1d3e4f5a6b',
    incident_id: 'inc-fda_enforcement-f-2291-2026',
    generated_at: '2026-09-13T10:04:11Z',
    content_hash: HEAD_1,
    hash_algorithm: 'sha256',
    timestamp_proof: 'MIIFxTADAgEAMIIFvAYJKoZIhvcNAQcCoIIFrTCCBakCAQMxDTALBglghkgBZQMEAgE=',
    event_count: 7,
    opened_at: '2026-09-13T08:02:00Z',
    closed_at: '2026-09-13T10:03:58Z',
    summary: {
        hazard: 'Undeclared peanut',
        classification: 'Class I',
        sources: ['fda_enforcement F-2291-2026'],
        products: ['Sunfield Farms Chewy Granola Bars 12 ct (00041196910537)'],
        lots_held: ['8H-1132', '8H-1140'],
        scope: 'LOT',
        decision: 'HUMAN_CONFIRMED',
        decided_by: 'ops:dana',
        confidence: 0.72,
        threshold: 0.85,
        units_held: 65,
        units_left_sellable: 60,
        customers_offered: 2,
        customers_answered: 1,
        notifications_sent: 3,
        evasion_flags: 0,
        redacted_fields: ['customer.email', 'customer.phone'],
    },
    timeline: [
        {
            seq: 1,
            at: '2026-09-13T08:02:00Z',
            actor: 'resolution-service',
            event: 'resolution.lot.resolved.v1',
            detail: 'resolved to 1 product(s), scope LOT, confidence 0.72',
        },
        {
            seq: 2,
            at: '2026-09-13T08:02:03Z',
            actor: 'containment-service',
            event: 'containment.action.proposed.v1',
            detail: 'queued for human review: confidence 0.72 below lot threshold 0.85',
        },
        {
            seq: 3,
            at: '2026-09-13T09:41:00Z',
            actor: 'containment-service',
            event: 'containment.action.taken.v1',
            detail: 'HUMAN_CONFIRMED by ops:dana: 65 units held, 60 left sellable',
        },
        {
            seq: 4,
            at: '2026-09-13T09:41:06Z',
            actor: 'order-rescue-service',
            event: 'rescue.order.proposed.v1',
            detail: 'order ORD-1001: 3 option(s) offered for lot 8H-1132',
        },
        {
            seq: 5,
            at: '2026-09-13T09:41:09Z',
            actor: 'notification-service',
            event: 'notification.delivered.v1',
            detail: 'SENT via email',
        },
        {
            seq: 6,
            at: '2026-09-13T10:01:22Z',
            actor: 'order-rescue-service',
            event: 'rescue.order.confirmed.v1',
            detail: 'order ORD-1001: customer chose sub-00012345678905',
        },
        {
            seq: 7,
            at: '2026-09-13T10:03:58Z',
            actor: 'notification-service',
            event: 'notification.delivered.v1',
            detail: 'SENT via email',
        },
    ],
}

/**
 * The realistic case: the default timestamp authority is reached over plain HTTP
 * and fails on any offline or egress-restricted machine, so the dossier carries
 * a note instead of a token. Counters that nothing feeds sit at zero.
 */
export const dossierWithNote: Dossier = {
    dossier_id: '3b7e9d41-0c5a-4e18-8f22-7d6c5b4a3e21',
    incident_id: 'inc-usda_fsis-031-2026',
    generated_at: '2026-09-13T07:15:40Z',
    content_hash: HEAD_2,
    hash_algorithm: 'sha256',
    timestamp_note: 'timestamp authority unavailable: Post "http://timestamp.digicert.com": dial tcp: lookup timestamp.digicert.com: no such host',
    event_count: 5,
    opened_at: '2026-09-13T06:40:00Z',
    closed_at: '2026-09-13T07:15:31Z',
    summary: {
        hazard: 'Listeria monocytogenes',
        sources: ['usda_fsis 031-2026'],
        products: ['Northcrest Turkey Slices 8 oz (00099482434523)'],
        lots_held: ['L-4471', 'L-4472'],
        scope: 'LOT',
        decision: 'AUTO_HOLD',
        confidence: 0.93,
        threshold: 0.85,
        units_held: 120,
        units_left_sellable: 0,
        customers_offered: 0,
        customers_answered: 0,
        notifications_sent: 0,
        evasion_flags: 0,
    },
    timeline: [
        {
            seq: 1,
            at: '2026-09-13T06:40:00Z',
            actor: 'resolution-service',
            event: 'resolution.lot.resolved.v1',
            detail: 'resolved to 1 product(s), scope LOT, confidence 0.93',
        },
        {
            seq: 2,
            at: '2026-09-13T06:40:04Z',
            actor: 'containment-service',
            event: 'containment.action.taken.v1',
            detail: 'AUTO_HOLD by system: 120 units held, 0 left sellable',
        },
    ],
}

export const verifiedOK: Verification = {
    incident_id: 'inc-fda_enforcement-f-2291-2026',
    verified: true,
    events: 7,
    content_hash: HEAD_1,
    hash_algorithm: 'sha256',
}

/**
 * A failed verification omits content_hash and hash_algorithm entirely — the
 * handler builds a different map on that branch. `problem` names only the first
 * bad record.
 */
export const verifiedBroken: Verification = {
    incident_id: 'inc-usda_fsis-031-2026',
    verified: false,
    events: 5,
    problem:
        'record 3 (containment.action.taken.v1): content altered, hash is a1b2c3d4 but recomputes to 99887766',
}

export const chainFixture: Chain = {
    incident_id: 'inc-eu_rasff-2026-0917',
    head: HEAD_3,
    opened_at: '2026-09-12T21:30:00Z',
    updated_at: '2026-09-12T21:31:12Z',
    records: [],
}
