import type { LotStatus } from '../../api/resolution'

type Props = {
    status: LotStatus | null
    loading?: boolean
    error?: boolean
}

/**
 * SafeLotBadge is the verdict on a lot. A safe lot gets one quiet line; an
 * affected lot gets the headline treatment, because that is the one moment
 * the store must not be subtle.
 */
export function SafeLotBadge({ status, loading, error }: Props) {
    if (loading) {
        return (
            <p className="text-body-sm text-pebble">
                <span className="dot mr-2 animate-pulse align-middle" />
                Checking this lot…
            </p>
        )
    }

    if (error || !status) {
        return (
            <p className="text-body-sm text-pebble">
                <span className="dot mr-2 align-middle" />
                We couldn’t check this lot right now.
            </p>
        )
    }

    switch (status.verdict) {
        case 'SAFE':
            return (
                <p className="text-body-sm">
                    <span className="dot dot-ok mr-2 align-middle" />
                    Lot <span className="mono">{status.lot_code}</span> verified — no active recall.
                </p>
            )

        case 'AFFECTED':
            return (
                <div>
                    <div className="wash-strip mb-4 w-16" />
                    <p className="headline text-ember-orange">Recalled lot — do not consume.</p>
                    {status.hazard && <p className="mt-3 text-body-lg">{status.hazard}</p>}
                    <p className="mt-2 text-body-sm text-pebble">
                        Lot <span className="mono">{status.lot_code}</span>. Return it for a full refund,
                        or throw it away. You don’t need a receipt.
                    </p>
                </div>
            )

        case 'UNKNOWN_LOT':
            return (
                <p className="text-body-sm text-pebble">
                    <span className="dot mr-2 align-middle" />
                    We don’t recognise lot <span className="mono">{status.lot_code}</span>. There is an
                    active recall on this product, so please don’t use it until we’ve confirmed.
                </p>
            )
    }
}
