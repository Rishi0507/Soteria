import { useEffect, useState } from 'react'
import { getLotStatus, type LotStatus } from '../../api/resolution'

type State = {
    status: LotStatus | null
    loading: boolean
    error: boolean
}

export function useLotStatus(gtin?: string, lotCode?: string): State {
    const [state, setState] = useState<State>({
        status: null,
        loading: Boolean(gtin),
        error: false,
    })

    useEffect(() => {
        if (!gtin) {
            setState({ status: null, loading: false, error: false })
            return
        }

        let cancelled = false
        setState({ status: null, loading: true, error: false })

        getLotStatus(gtin, lotCode)
            .then((status) => {
                if (!cancelled) setState({ status, loading: false, error: false })
            })
            .catch(() => {
                if (!cancelled) setState({ status: null, loading: false, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [gtin, lotCode])

    return state
}