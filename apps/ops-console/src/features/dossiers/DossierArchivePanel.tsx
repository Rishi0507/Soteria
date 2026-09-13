import type { ArchiveRow } from '../../api/audit'
import { shortHash } from './format'
import { VolatileLedgerNote } from './VolatileLedgerNote'

type Props = {
    rows: ArchiveRow[]
    selectedId?: string
    loading?: boolean
    error?: boolean
    onSelect: (incidentId: string) => void
    onReload?: () => void
}

/**
 * DossierArchivePanel lists recorded incidents.
 *
 * The list comes from the ledger, not from the dossiers: an incident appears as
 * soon as one event is recorded against it, whether or not anyone has built a
 * document for it. `has_dossier: false` is an ordinary state, not a fault.
 */
export function DossierArchivePanel({
    rows,
    selectedId,
    loading,
    error,
    onSelect,
    onReload,
}: Props) {
    return (
        <section className="block">
            <header className="border-b border-transparent p-4">
                <h2 className="text-heading-2xs font-normal text-bone-white">Incident archive</h2>
                <p className="mt-1 text-ui text-ash-gray">
                    Every incident with events on record, and whether a dossier has been built
                    for it.
                </p>
            </header>

            {loading && (
                <p className="p-5 text-ui text-ash-gray">Loading the archive&hellip;</p>
            )}

            {!loading && error && (
                <div className="p-5 text-ui text-silver-mist">
                    <p className="font-normal">We can&rsquo;t read the archive.</p>
                    <p className="mt-1 text-ash-gray">
                        This says nothing about whether the records exist — only that the audit
                        service did not answer.
                    </p>
                    {onReload && (
                        <button
                            type="button"
                            onClick={onReload}
                            className="mt-3 btn-ghost"
                        >
                            Try again
                        </button>
                    )}
                </div>
            )}

            {!loading && !error && rows.length === 0 && (
                <div className="p-5">
                    <p className="text-ui font-normal text-bone-white">
                        Nothing is recorded.
                    </p>
                    <VolatileLedgerNote context="empty" />
                    {onReload && (
                        <button
                            type="button"
                            onClick={onReload}
                            className="mt-3 btn-ghost"
                        >
                            Check again
                        </button>
                    )}
                </div>
            )}

            {!loading && !error && rows.length > 0 && (
                <ul className="divide-y divide-white/10">
                    {rows.map((row) => (
                        <li key={row.incident_id}>
                            <button
                                type="button"
                                onClick={() => onSelect(row.incident_id)}
                                aria-current={row.incident_id === selectedId}
                                className={
                                    'w-full px-4 py-3 text-left hover:bg-white/5 ' +
                                    (row.incident_id === selectedId ? 'bg-white/5' : '')
                                }
                            >
                                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                                    {row.has_dossier ? (
                                        <span className="chip chip-verdant uppercase tracking-wide text-verdant-text">
                                            dossier built
                                        </span>
                                    ) : (
                                        <span className="rounded-full px-2 py-0.5 text-caption font-normal uppercase tracking-wide text-bone-white">
                                            not generated
                                        </span>
                                    )}
                                    <span className="font-normal text-bone-white">
                                        {row.incident_id}
                                    </span>
                                    <span className="ml-auto text-caption text-ash-gray tabular-nums">
                                        {row.event_count === 1
                                            ? '1 event'
                                            : `${row.event_count} events`}
                                    </span>
                                </div>
                                {row.content_hash && (
                                    <p className="mt-1 font-mono text-caption text-ash-gray">
                                        chain head {shortHash(row.content_hash)}
                                    </p>
                                )}
                            </button>
                        </li>
                    ))}
                </ul>
            )}

            {!loading && !error && rows.length > 0 && (
                <footer className="border-t border-transparent px-4 py-3">
                    <VolatileLedgerNote context="list" />
                </footer>
            )}
        </section>
    )
}
