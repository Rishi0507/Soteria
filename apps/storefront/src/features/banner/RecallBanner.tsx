import { Link } from 'react-router-dom'
import type { LotStatus } from '../../api/resolution'

type Props = {
    status: LotStatus | null
}

/**
 * RecallBanner is the site-wide notice. It appears only for a verdict we can
 * stand behind: an affected lot, or one we could not identify. A safe lot
 * needs no banner.
 *
 * The AFFECTED banner is not dismissible. Letting someone click away a "do
 * not consume" notice is the one piece of UI politeness that could hurt them.
 * It is also the only place the ember accent is allowed to fill a surface.
 */
export function RecallBanner({ status }: Props) {
    if (!status || status.verdict === 'SAFE') return null

    if (status.verdict === 'UNKNOWN_LOT') {
        return (
            <div role="status" className="mx-auto max-w-[1440px] px-6 pb-6">
                <div className="hairline-dark" />
                <p className="pt-3 text-body-sm">
                    There is an active recall on this product and we don’t recognise the lot code you
                    entered. Please don’t consume it until we’ve confirmed.
                </p>
            </div>
        )
    }

    return (
        <div role="alert" className="wash">
            <div className="mx-auto flex max-w-[1440px] flex-wrap items-baseline gap-x-6 gap-y-1 px-6 py-3 text-body-sm">
                <span>Recall — do not consume this product.</span>
                {status.hazard && <span className="opacity-90">{status.hazard}</span>}
                {status.lot_code && <span className="mono">lot {status.lot_code}</span>}
                <Link to="/orders" className="link ml-auto text-pure-white">
                    See your options
                </Link>
            </div>
        </div>
    )
}
