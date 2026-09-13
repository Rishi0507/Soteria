import { useCallback, useEffect, useState } from 'react'
import {
    getContainmentConfig,
    updateContainmentConfig,
    InvalidBodyError,
    InvalidThresholdError,
    type ConfigUpdate,
    type ContainmentConfig,
} from '../../api/containment'

/** The outcome of a load, tagged with the inputs that produced it. */
type Loaded = {
    key: string
    config: ContainmentConfig | null
    error: boolean
}

/** What the panel should tell the operator about the last save attempt. */
export type Notice = {
    kind: 'saved' | 'rejected' | 'failed'
    text: string
}

export function useThresholds() {
    // There are no query inputs here, so the reload counter is what a result is
    // tagged with: a result fetched for an earlier token is not this request's
    // result and reads as loading rather than as an answer.
    const [reloadToken, setReloadToken] = useState(0)
    const key = String(reloadToken)

    const [loaded, setLoaded] = useState<Loaded | null>(null)
    const [saving, setSaving] = useState(false)
    const [notice, setNotice] = useState<Notice | null>(null)

    useEffect(() => {
        let cancelled = false

        getContainmentConfig()
            .then((config) => {
                if (!cancelled) setLoaded({ key, config, error: false })
            })
            .catch(() => {
                if (!cancelled) setLoaded({ key, config: null, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key])

    const current = loaded && loaded.key === key ? loaded : null
    const config = current?.config ?? null
    const loadError = current?.error ?? false
    const loading = current === null

    const reload = useCallback(() => {
        setNotice(null)
        setReloadToken((n) => n + 1)
    }, [])

    /** save only ever runs from a submit. A refusal is surfaced, never retried. */
    const save = useCallback(
        async (update: ConfigUpdate) => {
            setSaving(true)
            setNotice(null)
            try {
                const updated = await updateContainmentConfig(update)
                setLoaded({ key, config: updated, error: false })
                setNotice({
                    kind: 'saved',
                    text: 'Thresholds updated. Containment decisions from now on use these values.',
                })
            } catch (err) {
                if (
                    err instanceof InvalidThresholdError ||
                    err instanceof InvalidBodyError
                ) {
                    // The service's own wording, not a paraphrase: it knows which
                    // bound it applied, and this UI should not guess.
                    setNotice({ kind: 'rejected', text: err.message })
                } else {
                    setNotice({
                        kind: 'failed',
                        text: 'The containment service did not respond. Nothing was changed.',
                    })
                }
            } finally {
                setSaving(false)
            }
        },
        [key]
    )

    return { config, loading, loadError, saving, notice, save, reload }
}
