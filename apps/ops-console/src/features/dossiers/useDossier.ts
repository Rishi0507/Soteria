import { useCallback, useEffect, useState } from 'react'
import {
    generateDossier,
    getDossier,
    verifyChain,
    AuditServiceError,
    NotGeneratedError,
    NotRecordedError,
    type Dossier,
    type Verification,
} from '../../api/audit'

/**
 * What a load found. `state` matters as much as the dossier: the list is built
 * from the ledger, so "recorded but never generated" is a normal row, not a
 * failure, and must not be rendered as a 404.
 */
type Loaded = {
    key: string
    dossier: Dossier | null
    state: 'ready' | 'not-generated' | 'not-recorded' | 'error'
}

/** The outcome of a verification, tagged with the inputs that produced it. */
type Verified = {
    key: string
    verification: Verification | null
    error: boolean
}

export type DossierNotice = {
    kind: 'generated' | 'failed'
    text: string
}

export function useDossier(incidentId: string | undefined) {
    const [reloadToken, setReloadToken] = useState(0)
    const key = `${incidentId ?? ''}|${reloadToken}`

    const [loaded, setLoaded] = useState<Loaded | null>(null)
    const [generating, setGenerating] = useState(false)
    const [notice, setNotice] = useState<DossierNotice | null>(null)

    // Verification is a separate request and a separate answer: a chain can be
    // verifiable with no dossier built, and a dossier can exist over a chain
    // that no longer verifies. Tagged the same way, so a result for a different
    // incident reads as still-checking rather than as this one's answer.
    const [verified, setVerified] = useState<Verified | null>(null)

    useEffect(() => {
        if (!incidentId) return
        let cancelled = false

        getDossier(incidentId)
            .then((dossier) => {
                if (!cancelled) setLoaded({ key, dossier, state: 'ready' })
            })
            .catch((err) => {
                if (cancelled) return
                if (err instanceof NotGeneratedError) {
                    setLoaded({ key, dossier: null, state: 'not-generated' })
                } else if (err instanceof NotRecordedError) {
                    setLoaded({ key, dossier: null, state: 'not-recorded' })
                } else {
                    setLoaded({ key, dossier: null, state: 'error' })
                }
            })

        return () => {
            cancelled = true
        }
    }, [key, incidentId])

    // Verify alongside the load, so the integrity answer is on screen without
    // anyone having to ask for it.
    useEffect(() => {
        if (!incidentId) return
        let cancelled = false

        verifyChain(incidentId)
            .then((verification) => {
                if (!cancelled) setVerified({ key, verification, error: false })
            })
            .catch(() => {
                if (!cancelled) setVerified({ key, verification: null, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key, incidentId])

    const current = loaded && loaded.key === key ? loaded : null
    const loading = Boolean(incidentId) && current === null

    const currentVerified = verified && verified.key === key ? verified : null

    const reload = useCallback(() => {
        setNotice(null)
        setReloadToken((n) => n + 1)
    }, [])

    /**
     * generate is a write with side effects beyond this screen: a new dossier_id,
     * a re-rendered PDF and another audit.dossier.generated.v1 on the bus, even
     * when the chain has not moved. It only ever runs from an explicit click.
     */
    const generate = useCallback(async () => {
        if (!incidentId) return
        setGenerating(true)
        setNotice(null)
        try {
            const dossier = await generateDossier(incidentId)
            setLoaded({ key, dossier, state: 'ready' })
            setNotice({
                kind: 'generated',
                text: 'Generated. A new dossier id was issued and an event was published.',
            })
        } catch (err) {
            if (err instanceof NotRecordedError) {
                setNotice({
                    kind: 'failed',
                    text: 'No events are recorded for this incident, so there is nothing to build a dossier from.',
                })
            } else if (err instanceof AuditServiceError) {
                setNotice({ kind: 'failed', text: err.message })
            } else {
                setNotice({
                    kind: 'failed',
                    text: 'The audit service did not respond. Nothing was generated.',
                })
            }
        } finally {
            setGenerating(false)
        }
    }, [incidentId, key])

    return {
        dossier: current?.dossier ?? null,
        state: current?.state ?? 'ready',
        loading,
        generating,
        notice,
        verification: currentVerified?.verification ?? null,
        verifying: Boolean(incidentId) && currentVerified === null,
        verifyError: currentVerified?.error ?? false,
        generate,
        reload,
    }
}
