import { useCallback, useEffect, useState } from 'react'
import {
    confirmRescue,
    getRescue,
    listRescues,
    ConsentRejectedError,
    RescueClosedError,
    type Rescue,
} from '../../api/rescue'

type Args = {
    /** rescueId from the emailed link. */
    rescueId?: string
    /** orderId is the fallback lookup when the customer arrives from the site. */
    orderId?: string
    consentToken?: string
}

/** The outcome of a load, tagged with the inputs that produced it. */
type Loaded = {
    key: string
    rescue: Rescue | null
    error: boolean
}

export function useRescue({ rescueId, orderId, consentToken }: Args) {
    const key = `${rescueId ?? ''}|${orderId ?? ''}`

    const [loaded, setLoaded] = useState<Loaded | null>(null)
    /** confirming holds the option id in flight, so only that button goes pending. */
    const [confirming, setConfirming] = useState<string | null>(null)
    /** message is customer-facing copy for a refused or already-made choice. */
    const [message, setMessage] = useState<string | null>(null)

    useEffect(() => {
        if (!rescueId && !orderId) return

        let cancelled = false

        const load = rescueId
            ? getRescue(rescueId)
            : listRescues(orderId!).then((items) => items[0] ?? null)

        load
            .then((rescue) => {
                if (!cancelled) setLoaded({ key, rescue, error: false })
            })
            .catch(() => {
                if (!cancelled) setLoaded({ key, rescue: null, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key, rescueId, orderId])

    // A result loaded for different inputs is not this request's result;
    // it reads as loading rather than as an answer.
    const current = loaded && loaded.key === key ? loaded : null
    const rescue = current?.rescue ?? null
    const error = current?.error ?? false
    const loading = Boolean(rescueId || orderId) && current === null

    /**
     * confirm only ever runs from a click. Nothing here decides for the customer,
     * and a refusal is surfaced rather than retried.
     */
    const confirm = useCallback(
        async (optionId: string) => {
            const id = rescue?.rescue_id ?? rescueId
            if (!id) return
            if (!consentToken) {
                setMessage(
                    'This page is missing its confirmation token. Please use the link from your email.'
                )
                return
            }

            setConfirming(optionId)
            setMessage(null)
            try {
                const confirmed = await confirmRescue(id, optionId, consentToken)
                setLoaded({ key, rescue: confirmed, error: false })
                setConfirming(null)
            } catch (err) {
                const text =
                    err instanceof ConsentRejectedError || err instanceof RescueClosedError
                        ? err.message
                        : 'We could not record that choice. Please try again.'

                // On a conflict, re-read: the choice may have been made in another
                // tab or straight from the email link.
                if (err instanceof RescueClosedError) {
                    try {
                        const fresh = await getRescue(id)
                        setLoaded({ key, rescue: fresh, error: false })
                        setConfirming(null)
                        setMessage(text)
                        return
                    } catch {
                        // fall through to the plain message
                    }
                }
                setConfirming(null)
                setMessage(text)
            }
        },
        [key, rescue?.rescue_id, rescueId, consentToken]
    )

    return { rescue, loading, error, confirming, message, confirm }
}