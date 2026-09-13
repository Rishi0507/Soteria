import type { components } from './containment.types'

export type ContainmentConfig = components['schemas']['ContainmentConfig']
type ApiError = components['schemas']['Error']

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
