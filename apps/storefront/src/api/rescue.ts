import type { components } from './rescue.types'

export type Rescue = components['schemas']['Rescue']
export type RescueOption = components['schemas']['RescueOption']
export type AffectedLine = components['schemas']['AffectedLine']
export type Money = components['schemas']['Money']

const BASE = import.meta.env.VITE_RESCUE_API_URL

/** Lot code the service uses when it cannot pin the affected lot to one line. */
export const LOT_UNATTRIBUTED = 'UNATTRIBUTED'

export class RescueClosedError extends Error {}
export class ConsentRejectedError extends Error {}

export async function getRescue(rescueId: string): Promise<Rescue> {
    const res = await fetch(`${BASE}/v1/rescues/${rescueId}`)
    if (!res.ok) {
        throw new Error(`Rescue lookup failed: ${res.status}`)
    }
    return res.json()
}

export async function listRescues(orderId: string): Promise<Rescue[]> {
    const params = new URLSearchParams({ order_id: orderId })
    const res = await fetch(`${BASE}/v1/rescues?${params}`)
    if (!res.ok) {
        throw new Error(`Rescue list failed: ${res.status}`)
    }
    const body = (await res.json()) as { items?: Rescue[] | null }
    return body.items ?? []
}

/**
 * Confirms one option. The consent token arrives with the emailed link; the
 * service refuses a confirmation without it, so a swap can never be applied on
 * anyone's behalf but the customer's.
 */
export async function confirmRescue(
    rescueId: string,
    optionId: string,
    consentToken: string
): Promise<Rescue> {
    const res = await fetch(`${BASE}/v1/rescues/${rescueId}/confirm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ option_id: optionId, consent_token: consentToken }),
    })

    if (res.status === 403) {
        throw new ConsentRejectedError('This confirmation link is not valid.')
    }
    if (res.status === 409) {
        throw new RescueClosedError(
            'This choice has already been made, or the offer has expired.'
        )
    }
    if (!res.ok) {
        throw new Error(`Confirmation failed: ${res.status}`)
    }
    return res.json()
}

export function formatMoney(money?: Money): string {
    if (!money) return ''
    return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: money.currency,
    }).format(money.amount_minor / 100)
}
