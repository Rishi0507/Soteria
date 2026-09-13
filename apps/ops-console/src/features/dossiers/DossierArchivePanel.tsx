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
        <section className="rounded-xl border border-neutral-200 bg-white">
            <header className="border-b border-neutral-100 p-4">
                <h2 className="text-lg font-semibold text-neutral-900">Incident archive</h2>
                <p className="mt-1 text-sm text-neutral-600">
                    Every incident with events on record, and whether a dossier has been built
                    for it.
                </p>
            </header>

            {loading && (
                <p className="p-5 text-sm text-neutral-500">Loading the archive&hellip;</p>
            )}

            {!loading && error && (
                <div className="p-5 text-sm text-neutral-700">
                    <p className="font-medium">We can&rsquo;t read the archive.</p>
                    <p className="mt-1 text-neutral-600">
                        This says nothing about whether the records exist — only that the audit
                        service did not answer.
                    </p>
                    {onReload && (
                        <button
                            type="button"
                            onClick={onReload}
                            className="mt-3 rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50"
                        >
                            Try again
                        </button>
                    )}
                </div>
            )}

            {!loading && !error && rows.length === 0 && (
                <div className="p-5">
                    <p className="text-sm font-medium text-neutral-900">
                        Nothing is recorded.
                    </p>
                    <VolatileLedgerNote context="empty" />
                    {onReload && (
                        <button
                            type="button"
                            onClick={onReload}
                            className="mt-3 rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50"
                        >
                            Check again
                        </button>
                    )}
                </div>
            )}

            {!loading && !error && rows.length > 0 && (
                <ul className="divide-y divide-neutral-100">
                    {rows.map((row) => (
                        <li key={row.incident_id}>
                            <button
                                type="button"
                                onClick={() => onSelect(row.incident_id)}
                                aria-current={row.incident_id === selectedId}
                                className={
                                    'w-full px-4 py-3 text-left hover:bg-neutral-50 ' +
                                    (row.incident_id === selectedId ? 'bg-neutral-50' : '')
                                }
                            >
                                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                                    {row.has_dossier ? (
                                        <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium uppercase tracking-wide text-emerald-900">
                                            dossier built
                                        </span>
                                    ) : (
                                        <span className="rounded-full bg-neutral-200 px-2 py-0.5 text-xs font-medium uppercase tracking-wide text-neutral-800">
                                            not generated
                                        </span>
                                    )}
                                    <span className="font-medium text-neutral-900">
                                        {row.incident_id}
                                    </span>
                                    <span className="ml-auto text-xs text-neutral-500 tabular-nums">
                                        {row.event_count === 1
                                            ? '1 event'
                                            : `${row.event_count} events`}
                                    </span>
                                </div>
                                {row.content_hash && (
                                    <p className="mt-1 font-mono text-xs text-neutral-500">
                                        chain head {shortHash(row.content_hash)}
                                    </p>
                                )}
                            </button>
                        </li>
                    ))}
                </ul>
            )}

            {!loading && !error && rows.length > 0 && (
                <footer className="border-t border-neutral-100 px-4 py-3">
                    <VolatileLedgerNote context="list" />
                </footer>
            )}
        </section>
    )
}
