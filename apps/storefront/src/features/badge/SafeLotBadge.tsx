import type { LotStatus } from '../../api/resolution'

type Props = {
    status: LotStatus | null
    loading?: boolean
    error?: boolean
}

export function SafeLotBadge({ status, loading, error }: Props) {
    if (loading) {
        return (
            <span className="inline-flex items-center gap-2 rounded-full bg-neutral-100 px-3 py-1 text-sm text-neutral-500">
                Checking lot…
            </span>
        )
    }

    if (error || !status) {
        return (
            <span className="inline-flex items-center gap-2 rounded-full bg-neutral-100 px-3 py-1 text-sm text-neutral-600">
                Lot status unavailable
            </span>
        )
    }

    switch (status.verdict) {
        case 'SAFE':
            return (
                <span className="inline-flex items-center gap-2 rounded-full bg-emerald-50 px-3 py-1 text-sm font-medium text-emerald-800">
                    Verified Safe Lot
                    {status.lot_code && (
                        <span className="font-normal text-emerald-700">
                            {status.lot_code}
                        </span>
                    )}
                </span>
            )

        case 'AFFECTED':
            return (
                <span className="inline-flex flex-col gap-0.5 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-900">
                    <span className="font-semibold">Recalled lot — do not consume</span>
                    {status.hazard && (
                        <span className="text-red-800">{status.hazard}</span>
                    )}
                </span>
            )

        case 'UNKNOWN_LOT':
            return (
                <span className="inline-flex items-center gap-2 rounded-full bg-amber-50 px-3 py-1 text-sm text-amber-900">
                    Lot not recognised — not yet verified
                </span>
            )
    }
}