import { useCallback, useEffect, useState } from 'react'
import {
    confirmContainmentAction,
    getContainmentAction,
    rejectContainmentAction,
    ConflictError,
    InvalidBodyError,
    NotFoundError,
    type ConfirmRequest,
    type ContainmentAction,
    type RejectRequest,
} from '../../api/containment'

type Loaded = {
    key: string
    action: ContainmentAction | null
    error: boolean
}

export type DecisionNotice = {
    /**
     * conflict is its own kind because it is not really a failure: someone else
     * decided first, and the action on screen has been replaced with what it
     * actually became.
     */
    kind: 'decided' | 'partial' | 'conflict' | 'rejected-request' | 'failed'
    text: string
}

export function useActionReview(actionId: string | undefined) {
    const [reloadToken, setReloadToken] = useState(0)
    const key = `${actionId ?? ''}|${reloadToken}`

    const [loaded, setLoaded] = useState<Loaded | null>(null)
    const [deciding, setDeciding] = useState(false)
    const [notice, setNotice] = useState<DecisionNotice | null>(null)

    useEffect(() => {
        if (!actionId) return
        let cancelled = false

        getContainmentAction(actionId)
            .then((action) => {
                if (!cancelled) setLoaded({ key, action, error: false })
            })
            .catch(() => {
                if (!cancelled) setLoaded({ key, action: null, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key, actionId])

    const current = loaded && loaded.key === key ? loaded : null
    const action = current?.action ?? null
    const error = current?.error ?? false
    const loading = Boolean(actionId) && current === null

    const reload = useCallback(() => {
        setNotice(null)
        setReloadToken((n) => n + 1)
    }, [])

    /**
     * A 409 means the action left PENDING_REVIEW before we got there, almost
     * always because another reviewer decided first. Re-read it and show what it
     * became: retrying cannot succeed, and a rejection in particular is terminal.
     */
    const handleConflict = useCallback(
        async (id: string) => {
            try {
                const fresh = await getContainmentAction(id)
                setLoaded({ key, action: fresh, error: false })
                setNotice({
                    kind: 'conflict',
                    text: `Someone else decided this first. It is now ${fresh.status}${fresh.actor ? `, by ${fresh.actor}` : ''}. Your decision was not applied.`,
                })
            } catch {
                setNotice({
                    kind: 'conflict',
                    text: 'This action is no longer awaiting review, and it could not be re-read. Your decision was not applied.',
                })
            }
        },
        [key]
    )

    const decide = useCallback(
        async (
            id: string,
            run: () => Promise<ContainmentAction>,
            describe: (result: ContainmentAction) => DecisionNotice
        ) => {
            setDeciding(true)
            setNotice(null)
            try {
                const updated = await run()
                setLoaded({ key, action: updated, error: false })
                setNotice(describe(updated))
            } catch (err) {
                if (err instanceof ConflictError) {
                    await handleConflict(id)
                } else if (err instanceof InvalidBodyError) {
                    setNotice({ kind: 'rejected-request', text: err.message })
                } else if (err instanceof NotFoundError) {
                    setNotice({
                        kind: 'failed',
                        text: 'This action no longer exists. Nothing was changed.',
                    })
                } else {
                    setNotice({
                        kind: 'failed',
                        text: 'The containment service did not respond. Nothing was changed.',
                    })
                }
            } finally {
                setDeciding(false)
            }
        },
        [key, handleConflict]
    )

    const confirm = useCallback(
        (body: ConfirmRequest) => {
            if (!actionId) return Promise.resolve()
            return decide(actionId, () => confirmContainmentAction(actionId, body), (result) => {
                // Status alone is not the outcome: any target held makes the
                // action HUMAN_CONFIRMED even when other targets failed.
                const failed = (result.results ?? []).filter((r) => r.status === 'FAILED')
                if (failed.length > 0) {
                    return {
                        kind: 'partial',
                        text: `Confirmed, but ${failed.length} of ${(result.results ?? []).length} targets did not hold. Check each result below.`,
                    }
                }
                return { kind: 'decided', text: 'Confirmed. The hold has been placed.' }
            })
        },
        [actionId, decide]
    )

    const reject = useCallback(
        (body: RejectRequest) => {
            if (!actionId) return Promise.resolve()
            return decide(actionId, () => rejectContainmentAction(actionId, body), () => ({
                kind: 'decided',
                text: 'Rejected. Nothing was held, and this cannot be undone.',
            }))
        },
        [actionId, decide]
    )

    return { action, loading, error, deciding, notice, confirm, reject, reload }
}
