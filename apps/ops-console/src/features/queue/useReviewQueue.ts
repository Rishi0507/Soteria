import { useCallback, useEffect, useState } from 'react'
import {
    listContainmentActions,
    type ActionStatus,
    type ContainmentAction,
} from '../../api/containment'

/** The outcome of a load, tagged with the inputs that produced it. */
type Loaded = {
    key: string
    actions: ContainmentAction[]
    error: boolean
}

export function useReviewQueue(status: ActionStatus | undefined) {
    // The filter and the reload counter together are what a result is tagged
    // with: a list fetched for a different filter is not this request's answer
    // and reads as loading rather than as a stale queue.
    const [reloadToken, setReloadToken] = useState(0)
    const key = `${status ?? ''}|${reloadToken}`

    const [loaded, setLoaded] = useState<Loaded | null>(null)

    useEffect(() => {
        let cancelled = false

        listContainmentActions(status)
            .then((actions) => {
                if (!cancelled) setLoaded({ key, actions, error: false })
            })
            .catch(() => {
                if (!cancelled) setLoaded({ key, actions: [], error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key, status])

    const current = loaded && loaded.key === key ? loaded : null

    const reload = useCallback(() => setReloadToken((n) => n + 1), [])

    return {
        actions: current?.actions ?? [],
        loading: current === null,
        error: current?.error ?? false,
        reload,
    }
}
