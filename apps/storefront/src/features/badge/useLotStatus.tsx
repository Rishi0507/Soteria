import { useEffect, useState } from 'react'
import { getLotStatus, type LotStatus } from '../../api/resolution'

type State = {
    status: LotStatus | null
    loading: boolean
    error: boolean
}

/** Stores the inputs alongside the outcome so loading can be derived. */
type Result = {
    gtin: string
    lotCode?: string
    status: LotStatus | null
    error: boolean
}

export function useLotStatus(gtin?: string, lotCode?: string): State {
    const [result, setResult] = useState<Result | null>(null)

    useEffect(() => {
        if (!gtin) return

        let cancelled = false

        getLotStatus(gtin, lotCode)
            .then((status) => {
                if (!cancelled) setResult({ gtin, lotCode, status, error: false })
            })
            .catch(() => {
                if (!cancelled) setResult({ gtin, lotCode, status: null, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [gtin, lotCode])

    // A result for a different gtin/lot is not this lot's result. Treating it
    // as loading rather than as an answer matters here more than most places:
    // showing a stale SAFE verdict against a newly entered lot is exactly the
    // failure this badge exists to prevent.
    const current =
        result && result.gtin === gtin && result.lotCode === lotCode ? result : null

    return {
        status: current?.status ?? null,
        loading: Boolean(gtin) && current === null,
        error: current?.error ?? false,
    }
}