import type { ContainmentAction } from '../api/containment'

/**
 * Queue fixtures. Shapes follow ContainmentAction exactly: no units on a pending
 * action, because the service has none to give until a hold runs.
 */

/** Single LOT-scope target, two lots — the ordinary case. */
export const pendingSingle: ContainmentAction = {
    action_id: 'act-1001',
    incident_id: 'inc-fda_enforcement-f-2291-2026',
    status: 'PENDING_REVIEW',
    confidence: 0.72,
    threshold: 0.85,
    hazard: 'Undeclared peanut',
    reason: 'confidence 0.72 below lot threshold 0.85',
    created_at: '2026-09-13T08:10:00Z',
    targets: [
        {
            gtin: '00041196910537',
            sku: 'SF-GB-12',
            product_title: 'Sunfield Farms Chewy Granola Bars 12 ct',
            scope: 'LOT',
            lot_codes: ['8H-1132', '8H-1140'],
            confidence: 0.72,
        },
    ],
}

/** Two LOT-scope targets sharing a lot code — the flat-override trap. */
export const pendingMultiTarget: ContainmentAction = {
    action_id: 'act-1002',
    incident_id: 'inc-usda_fsis-031-2026',
    status: 'PENDING_REVIEW',
    confidence: 0.66,
    threshold: 0.85,
    hazard: 'Listeria monocytogenes',
    reason: 'confidence 0.66 below lot threshold 0.85',
    created_at: '2026-09-13T06:45:00Z',
    targets: [
        {
            gtin: '00099482434523',
            sku: 'NC-TK-8',
            product_title: 'Northcrest Turkey Slices 8 oz',
            scope: 'LOT',
            lot_codes: ['L-4471', 'L-4472'],
            confidence: 0.68,
        },
        {
            gtin: '00099482434530',
            sku: 'NC-TK-16',
            product_title: 'Northcrest Turkey Slices 16 oz',
            scope: 'LOT',
            lot_codes: ['L-4472', 'L-4489'],
            confidence: 0.64,
        },
    ],
}

/** SKU scope: no lot codes were recovered, so nothing can be narrowed. */
export const pendingSKUScope: ContainmentAction = {
    action_id: 'act-1003',
    incident_id: 'inc-eu_rasff-2026-0917',
    status: 'PENDING_REVIEW',
    confidence: 0.91,
    threshold: 0.95,
    hazard: 'Ethylene oxide in sesame',
    reason: 'confidence 0.91 below sku threshold 0.95',
    created_at: '2026-09-12T21:30:00Z',
    targets: [
        {
            gtin: '00074234661016',
            sku: 'TH-SES-10',
            product_title: 'Tahini House Sesame Paste 10 oz',
            scope: 'SKU',
            confidence: 0.91,
        },
    ],
}

/** Mixed: one narrowable target, one that can only be held whole. */
export const pendingMixedScope: ContainmentAction = {
    action_id: 'act-1004',
    incident_id: 'inc-fda_press-2026-0913',
    status: 'PENDING_REVIEW',
    confidence: 0.8,
    threshold: 0.85,
    hazard: 'Undeclared milk',
    reason: 'confidence 0.80 below lot threshold 0.85',
    created_at: '2026-09-13T09:55:00Z',
    targets: [
        {
            gtin: '00052000132632',
            sku: 'BB-CK-6',
            product_title: 'Bakers Bend Cookie Assortment 6 ct',
            scope: 'LOT',
            lot_codes: ['C-2210'],
            confidence: 0.82,
        },
        {
            gtin: '00052000132649',
            sku: 'BB-CK-12',
            product_title: 'Bakers Bend Cookie Assortment 12 ct',
            scope: 'SKU',
            confidence: 0.78,
        },
    ],
}

export const rejectedAction: ContainmentAction = {
    action_id: 'act-0990',
    incident_id: 'inc-fda_enforcement-f-2201-2026',
    status: 'HUMAN_REJECTED',
    confidence: 0.55,
    threshold: 0.85,
    hazard: 'Undeclared soy',
    // `reason` keeps the queueing explanation…
    reason: 'confidence 0.55 below lot threshold 0.85',
    actor: 'ops:sam',
    // …and the reviewer's own reason is stored in `note`.
    note: 'Notice refers to a different brand with a similar name.',
    created_at: '2026-09-12T14:00:00Z',
    decided_at: '2026-09-12T14:22:00Z',
    targets: [
        {
            gtin: '00028400090000',
            sku: 'RV-SY-5',
            product_title: 'Riverbend Soy Crisps 5 oz',
            scope: 'LOT',
            lot_codes: ['S-8801'],
            confidence: 0.55,
        },
    ],
}

/** Confirmed, but one target failed to hold — status alone says HUMAN_CONFIRMED. */
export const partialFailureAction: ContainmentAction = {
    ...pendingMultiTarget,
    status: 'HUMAN_CONFIRMED',
    actor: 'ops:dana',
    note: 'Confirmed against the FSIS notice.',
    decided_at: '2026-09-13T10:02:00Z',
    results: [
        {
            gtin: '00099482434523',
            status: 'HELD',
            platform: 'shopify',
            platform_ref: 'gid://shopify/InventoryItem/44120',
            units_held: 40,
            units_left_sellable: 60,
        },
        {
            gtin: '00099482434530',
            status: 'FAILED',
            platform: 'shopify',
            error: 'inventoryMoveQuantities: location Quarantine not found',
        },
    ],
}

export const queue: ContainmentAction[] = [
    pendingMixedScope,
    pendingSingle,
    pendingMultiTarget,
    pendingSKUScope,
    rejectedAction,
]
