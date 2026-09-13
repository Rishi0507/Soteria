import type { components } from './containment.types'

export type ContainmentConfig = components['schemas']['ContainmentConfig']
export type ContainmentAction = components['schemas']['ContainmentAction']
export type ContainmentTarget = components['schemas']['ContainmentTarget']
export type ContainmentResult = components['schemas']['ContainmentResult']
export type ActionStatus = ContainmentAction['status']
type ApiError = components['schemas']['Error']

export const ACTION_STATUSES: ActionStatus[] = [
    'PENDING_REVIEW',
    'AUTO_HELD',
    'HUMAN_CONFIRMED',
    'HUMAN_REJECTED',
    'FAILED',
]

const BASE = import.meta.env.VITE_CONTAINMENT_API_URL

/**
 * 400 / invalid_threshold — a value outside the range the service accepts.
 * The service's own bounds are narrower than the OpenAPI spec declares, so the
 * message is surfaced verbatim rather than reworded.
 */
export class InvalidThresholdError extends Error {}

/** 400 / invalid_body — actor missing, or the body did not parse. */
export class InvalidBodyError extends Error {}

/** Anything else: 5xx, or the service not answering at all. */
export class ServiceError extends Error {}

/** 404 — the action id is not one the service knows. */
export class NotFoundError extends Error {}

/**
 * 409 — the action was not PENDING_REVIEW when the service looked.
 *
 * Confirm and reject both accept only PENDING_REVIEW, so the overwhelmingly
 * likely cause is another reviewer deciding first. The caller is expected to
 * re-read the action and show what it actually became rather than retrying.
 */
export class ConflictError extends Error {}

/**
 * The narrowest value this UI offers for either threshold.
 *
 * Not the spec's `minimum: 0`. containment-service rejects auto_hold_threshold
 * at 0 outright, and treats sku_scope_threshold at 0 as "not supplied" — that
 * request returns 200 having changed nothing, which would show an operator a
 * success message for a save that did not happen. Neither field may offer 0.
 */
export const THRESHOLD_MIN = 0.01
export const THRESHOLD_MAX = 1
export const THRESHOLD_STEP = 0.01

export type ConfigUpdate = {
    auto_hold_threshold: number
    sku_scope_threshold: number
    actor: string
}

/**
 * A whole-SKU hold burns inventory that may be clean, so it is meant to carry a
 * higher bar than a lot-level hold. Nothing in the service enforces that; this
 * is the UI's only chance to show an operator they are giving that bar up.
 *
 * Equality counts. A whole-SKU threshold merely *equal* to the lot-level one
 * still fails the rule: pulling an entire product line becomes exactly as easy
 * as pulling one lot, when it is supposed to be harder. Strictly below is worse
 * in degree, not in kind, so both sit behind the same acknowledgement.
 */
export function isInverted(autoHold: number, skuScope: number): boolean {
    return skuScope <= autoHold
}

export async function getContainmentConfig(): Promise<ContainmentConfig> {
    const res = await fetch(`${BASE}/v1/containment/config`)
    if (!res.ok) {
        throw new ServiceError(`Config lookup failed: ${res.status}`)
    }
    return res.json()
}

export async function updateContainmentConfig(
    update: ConfigUpdate
): Promise<ContainmentConfig> {
    const res = await fetch(`${BASE}/v1/containment/config`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(update),
    })

    if (res.status === 400) {
        const body = (await res.json().catch(() => null)) as ApiError | null
        const message = body?.message ?? 'The service rejected these values.'
        if (body?.code === 'invalid_threshold') {
            throw new InvalidThresholdError(message)
        }
        throw new InvalidBodyError(message)
    }
    if (!res.ok) {
        throw new ServiceError(`Update failed: ${res.status}`)
    }
    return res.json()
}

// ------------------------------------------------------------- review queue

export type ConfirmRequest = {
    actor: string
    note?: string
    /**
     * Lots to keep holding. The service INTERSECTS this with each target's own
     * lots, so it can only ever narrow. Omit it to hold everything already in
     * scope; see targetsUnaffectedBySelection() for why it must never be typed
     * by hand.
     */
    lot_codes_override?: string[]
}

export type RejectRequest = {
    actor: string
    reason: string
}

export async function listContainmentActions(
    status?: ActionStatus,
    limit?: number
): Promise<ContainmentAction[]> {
    const params = new URLSearchParams()
    if (status) params.set('status', status)
    if (limit) params.set('limit', String(limit))
    const query = params.toString()

    const res = await fetch(
        `${BASE}/v1/containment/actions${query ? `?${query}` : ''}`
    )
    if (!res.ok) {
        throw new ServiceError(`Review queue lookup failed: ${res.status}`)
    }
    const body = (await res.json()) as { items?: ContainmentAction[] | null }
    return body.items ?? []
}

export async function getContainmentAction(
    actionId: string
): Promise<ContainmentAction> {
    const res = await fetch(`${BASE}/v1/containment/actions/${actionId}`)
    if (res.status === 404) {
        throw new NotFoundError('No such containment action.')
    }
    if (!res.ok) {
        throw new ServiceError(`Action lookup failed: ${res.status}`)
    }
    return res.json()
}

async function decide(
    actionId: string,
    path: 'confirm' | 'reject',
    body: ConfirmRequest | RejectRequest
): Promise<ContainmentAction> {
    const res = await fetch(
        `${BASE}/v1/containment/actions/${actionId}/${path}`,
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        }
    )

    if (res.status === 404) {
        throw new NotFoundError('No such containment action.')
    }
    if (res.status === 409) {
        throw new ConflictError('This action is no longer awaiting review.')
    }
    // Undocumented on these two operations, but the handler does return it when
    // actor (or, on reject, reason) is missing.
    if (res.status === 400) {
        const err = (await res.json().catch(() => null)) as ApiError | null
        throw new InvalidBodyError(err?.message ?? 'The service rejected this request.')
    }
    if (!res.ok) {
        throw new ServiceError(`Decision failed: ${res.status}`)
    }
    return res.json()
}

export function confirmContainmentAction(
    actionId: string,
    body: ConfirmRequest
): Promise<ContainmentAction> {
    return decide(actionId, 'confirm', body)
}

export function rejectContainmentAction(
    actionId: string,
    body: RejectRequest
): Promise<ContainmentAction> {
    return decide(actionId, 'reject', body)
}

// ------------------------------------------------------- narrowing helpers

/** A target whose scope is SKU carries no lot codes and cannot be narrowed. */
export function isNarrowable(target: ContainmentTarget): boolean {
    return target.scope === 'LOT' && (target.lot_codes?.length ?? 0) > 0
}

/**
 * Every distinct lot code on the action, in first-seen order.
 *
 * The override is one flat list applied to every target — there is no per-GTIN
 * addressing in the API — so this union, not any single target's list, is the
 * real unit of choice.
 */
export function lotUnion(targets: ContainmentTarget[]): string[] {
    const seen = new Set<string>()
    const out: string[] = []
    for (const t of targets) {
        for (const code of t.lot_codes ?? []) {
            const key = code.trim().toUpperCase()
            if (!seen.has(key)) {
                seen.add(key)
                out.push(code)
            }
        }
    }
    return out
}

/** Which targets carry a given lot code. Matching is case- and space-insensitive, as the service's is. */
export function targetsWithLot(
    targets: ContainmentTarget[],
    code: string
): ContainmentTarget[] {
    const key = code.trim().toUpperCase()
    return targets.filter((t) =>
        (t.lot_codes ?? []).some((l) => l.trim().toUpperCase() === key)
    )
}

/**
 * Targets that would keep their FULL lot list under this selection.
 *
 * This is the failure the UI exists to prevent. narrowLots only replaces a
 * target's lots when the intersection is non-empty; when it is empty the target
 * passes through untouched and the service answers 200 with no indication that
 * the override did nothing. A selection that empties any target is therefore not
 * a narrower hold, it is a silently wider one.
 */
export function targetsUnaffectedBySelection(
    targets: ContainmentTarget[],
    selected: string[]
): ContainmentTarget[] {
    const keys = new Set(selected.map((c) => c.trim().toUpperCase()))
    return targets
        .filter(isNarrowable)
        .filter(
            (t) =>
                !(t.lot_codes ?? []).some((l) => keys.has(l.trim().toUpperCase()))
        )
}

/** The lots a target would actually keep under this selection. */
export function keptLots(
    target: ContainmentTarget,
    selected: string[]
): string[] {
    const keys = new Set(selected.map((c) => c.trim().toUpperCase()))
    const kept = (target.lot_codes ?? []).filter((l) =>
        keys.has(l.trim().toUpperCase())
    )
    // An empty intersection means the service ignores the override for this
    // target and holds every lot it already had.
    return kept.length > 0 ? kept : (target.lot_codes ?? [])
}
