import type { LotStatus } from '../../api/resolution'

type Props = {
    status: LotStatus | null
    /** onReview scrolls the customer to the choice we are waiting on. */
    onReview?: () => void
}

/**
 * RecallBanner is the site-wide notice. It appears only for a verdict we can
 * stand behind: an affected lot, or one we could not identify. A safe lot needs
 * no banner, and an unidentified lot is told the truth rather than reassured.
 *
 * The AFFECTED banner is deliberately not dismissible. Letting someone click
 * away a "do not consume" notice is the one piece of UI politeness that could
 * actually hurt them.
 */
export function RecallBanner({ status, onReview }: Props) {
    if (!status || status.verdict === 'SAFE') return null

    if (status.verdict === 'UNKNOWN_LOT') {
        return (
            <div
                role="status"
                className="border-b border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
            >
                <div className="mx-auto max-w-5xl">
                    <span className="font-semibold">We can’t identify this lot.</span> There is
                    an active recall on this product and the code on your pack isn’t one we
                    recognise. Please don’t consume it until you’ve checked with us.
                </div>
            </div>
        )
    }

    return (
        <div role="alert" className="bg-red-600 px-4 py-3 text-sm text-white">
            <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-semibold">Recall: do not consume this product.</span>
                {status.hazard && <span className="text-red-50">{status.hazard}</span>}
                {status.lot_code && (
                    <span className="rounded bg-red-700 px-2 py-0.5 text-xs">
                        lot {status.lot_code}
                    </span>
                )}
                {onReview && (
                    <button
                        type="button"
                        onClick={onReview}
                        className="font-semibold underline underline-offset-2 hover:no-underline"
                    >
                        See your options
                    </button>
                )}
            </div>
        </div>
    )
}
