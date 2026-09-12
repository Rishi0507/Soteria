import { useCallback, useEffect, useState } from 'react'
import {
    confirmRescue,
    getRescue,
    listRescues,
    ConsentRejectedError,
    RescueClosedError,
    type Rescue,
} from '../../api/rescue'

type State = {
    rescue: Rescue | null
    loading: boolean
    error: boolean
    /** confirming holds the option id in flight, so only that button goes pending. */
    confirming: string | null
    /** message is customer-facing copy for a refused or already-made choice. */
    message: string | null
}

const idle: State = {
    rescue: null,
    loading: false,
    error: false,
    confirming: null,
    message: null,
}

type Args = {
    /** rescueId from the emailed link. */
    rescueId?: string
    /** orderId is the fallback lookup when the customer arrives from the site. */
    orderId?: string
    consentToken?: string
}

export function useRescue({ rescueId, orderId, consentToken }: Args) {
    const [state, setState] = useState<State>({
        ...idle,
        loading: Boolean(rescueId || orderId),
    })

    useEffect(() => {
        if (!rescueId && !orderId) {
            setState(idle)
            return
        }

        let cancelled = false
        setState({ ...idle, loading: true })

        const load = rescueId
            ? getRescue(rescueId)
            : listRescues(orderId!).then((items) => items[0] ?? null)

        load
            .then((rescue) => {
                if (!cancelled) setState({ ...idle, rescue })
            })
            .catch(() => {
                if (!cancelled) setState({ ...idle, error: true })
            })

        return () => {
            cancelled = true
        }
    }, [rescueId, orderId])

    /**
     * confirm only ever runs from a click. Nothing here decides for the customer,
     * and a refusal is surfaced rather than retried.
     */
    const confirm = useCallback(
        async (optionId: string) => {
            const id = state.rescue?.rescue_id ?? rescueId
            if (!id) return
            if (!consentToken) {
                setState((s) => ({
                    ...s,
                    message:
                        'This page is missing its confirmation token. Please use the link from your email.',
                }))
                return
            }

            setState((s) => ({ ...s, confirming: optionId, message: null }))
            try {
                const rescue = await confirmRescue(id, optionId, consentToken)
                setState({ ...idle, rescue })
            } catch (err) {
                const message =
                    err instanceof ConsentRejectedError || err instanceof RescueClosedError
                        ? err.message
                        : 'We could not record that choice. Please try again.'

                // On a conflict, re-read: the choice may have been made in another
                // tab or straight from the email link.
                if (err instanceof RescueClosedError) {
                    try {
                        const rescue = await getRescue(id)
                        setState({ ...idle, rescue, message })
                        return
                    } catch {
                        // fall through to the plain message
                    }
                }
                setState((s) => ({ ...s, confirming: null, message }))
            }
        },
        [state.rescue?.rescue_id, rescueId, consentToken]
    )

    return { ...state, confirm }
}
