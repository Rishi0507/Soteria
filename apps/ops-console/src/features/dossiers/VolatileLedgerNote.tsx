/**
 * The one statement this screen cannot leave out.
 *
 * The audit ledger is an in-memory map rebuilt empty on every start. Nothing in
 * any response separates "nothing has happened yet" from "the record was lost":
 * an empty list is byte-identical either way, and a 404 on an incident that
 * verified yesterday is byte-identical to a 404 on an id that never existed.
 *
 * For the service whose whole claim is that the record is complete, an empty
 * screen that reads like a clean slate is the worst available answer. So the
 * screen says which it cannot tell, rather than implying the reassuring one.
 */
export function VolatileLedgerNote({
    context,
}: {
    context: 'empty' | 'list' | 'missing'
}) {
    if (context === 'list') {
        return (
            <p className="text-caption text-ash-gray">
                The audit ledger is held in memory and does not survive a restart. This list
                shows what this process has recorded since it started — not necessarily
                everything that has ever happened.
            </p>
        )
    }

    if (context === 'missing') {
        return (
            <p className="mt-2 rounded-3xl px-3 py-2 text-ui text-saffron-spark">
                <span className="font-normal">
                    This may mean the incident was never recorded, or that its record was
                    lost.
                </span>{' '}
                The audit ledger is held in memory and is emptied by a restart. The service
                returns the same answer in both cases, so this screen cannot tell you which
                happened.
            </p>
        )
    }

    return (
        <p className="mt-2 rounded-3xl px-3 py-2 text-ui text-saffron-spark">
            <span className="font-normal">
                An empty archive is not evidence that nothing happened.
            </span>{' '}
            The audit ledger is held in memory and is emptied by a restart. If this service
            has restarted since the incidents you are looking for, their records are gone and
            nothing in the response distinguishes that from a genuinely quiet period.
        </p>
    )
}
