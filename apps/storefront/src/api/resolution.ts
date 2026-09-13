import type { components } from './resolution.types'

export type LotStatus = components['schemas']['LotStatus']
export type Resolution = components['schemas']['Resolution']

const BASE = import.meta.env.VITE_RESOLUTION_API_URL

export async function getLotStatus(
    gtin: string,
    lotCode?: string
): Promise<LotStatus> {
    const params = new URLSearchParams({ gtin })
    if (lotCode) params.set('lot_code', lotCode)

    const res = await fetch(`${BASE}/v1/lots/status?${params}`)
    if (!res.ok) {
        throw new Error(`Lot status lookup failed: ${res.status}`)
    }
    return res.json()
}