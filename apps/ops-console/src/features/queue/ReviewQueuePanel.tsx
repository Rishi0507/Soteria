import {
    ACTION_STATUSES,
    type ActionStatus,
    type ContainmentAction,
} from '../../api/containment'
import { ageLabel } from './age'

type Props = {
    actions: ContainmentAction[]
    status: ActionStatus | undefined
    selectedId?: string
    loading?: boolean
    error?: boolean
    onStatusChange: (status: ActionStatus | undefined) => void
    onSelect: (actionId: string) => void
    onReload?: () => void
}

/**
 * ReviewQueuePanel lists containment decisions. Server state arrives as props;
 * the only thing it owns is nothing at all — selection and filter live above it.
 */
export function ReviewQueuePanel({
    actions,
    status,
    selectedId,
    loading,
    error,
    onStatusChange,
    onSelect,
    onReload,
}: Props) {
    return (
        <section className="rounded-xl border border-neutral-200 bg-white">
            <header className="flex flex-wrap items-center justify-between gap-3 border-b border-neutral-100 p-4">
                <div>
                    <h2 className="text-lg font-semibold text-neutral-900">Review queue</h2>
                    <p className="mt-1 text-sm text-neutral-600">
                        Containment decisions that scored below the threshold in force when
                        they were made, and the history of those already decided.
                    </p>
                </div>
                <label className="text-sm">
                    <span className="sr-only">Filter by status</span>
                    <select
                        value={status ?? ''}
                        onChange={(e) =>
                            onStatusChange(
                                e.target.value === ''
                                    ? undefined
                                    : (e.target.value as ActionStatus)
                            )
                        }
                        className="rounded-lg border border-neutral-300 px-3 py-2 text-sm"
                    >
                        <option value="">All statuses</option>
                        {ACTION_STATUSES.map((s) => (
                            <option key={s} value={s}>
                                {s.replace(/_/g, ' ').toLowerCase()}
                            </option>
                        ))}
                    </select>
                </label>
            </header>

            {loading && (
                <p className="p-5 text-sm text-neutral-500">Loading the queue&hellip;</p>
            )}

            {!loading && error && (
                <div className="p-5 text-sm text-neutral-700">
                    <p className="font-medium">We can&rsquo;t read the review queue.</p>
                    <p className="mt-1 text-neutral-600">
                        Nothing has been decided. The containment service may be down.
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

            {!loading && !error && actions.length === 0 && (
                <p className="p-5 text-sm text-neutral-600">
                    {status
                        ? `Nothing with status ${status.replace(/_/g, ' ').toLowerCase()}.`
                        : 'Nothing in the queue. Every resolved recall so far cleared its threshold or has already been decided.'}
                </p>
            )}

            {!loading && !error && actions.length > 0 && (
                <ul className="divide-y divide-neutral-100">
                    {actions.map((action) => (
                        <li key={action.action_id}>
                            <button
                                type="button"
                                onClick={() => onSelect(action.action_id)}
                                aria-current={action.action_id === selectedId}
                                className={
                                    'w-full px-4 py-3 text-left hover:bg-neutral-50 ' +
                                    (action.action_id === selectedId ? 'bg-neutral-50' : '')
                                }
                            >
                                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                                    <StatusBadge status={action.status} />
                                    <span className="font-medium text-neutral-900">
                                        {action.hazard ?? 'Hazard not stated'}
                                    </span>
                                    <span className="text-xs text-neutral-500">
                                        {action.incident_id}
                                    </span>
                                    <span className="ml-auto text-xs text-neutral-500">
                                        {ageLabel(action.created_at)}
                                    </span>
                                </div>

                                <p className="mt-1 text-sm text-neutral-700">
                                    {action.reason ?? 'No queueing reason recorded.'}
                                </p>

                                <p className="mt-1 text-xs text-neutral-600">
                                    <span className="tabular-nums">
                                        confidence {action.confidence} vs threshold{' '}
                                        {action.threshold}
                                    </span>
                                    <span className="ml-2 text-neutral-500">
                                        (the cutoff in force when this was decided)
                                    </span>
                                </p>

                                <p className="mt-1 text-xs text-neutral-600">
                                    {action.targets.length === 1
                                        ? '1 product'
                                        : `${action.targets.length} products`}
                                    {': '}
                                    {action.targets
                                        .map((t) => t.product_title ?? t.sku ?? t.gtin)
                                        .join(', ')}
                                </p>
                            </button>
                        </li>
                    ))}
                </ul>
            )}
        </section>
    )
}

export function StatusBadge({ status }: { status: ContainmentAction['status'] }) {
    const tone =
        status === 'PENDING_REVIEW'
            ? 'bg-amber-100 text-amber-900'
            : status === 'HUMAN_REJECTED'
              ? 'bg-neutral-200 text-neutral-800'
              : status === 'FAILED'
                ? 'bg-red-100 text-red-900'
                : 'bg-emerald-100 text-emerald-900'

    return (
        <span
            className={`rounded-full px-2 py-0.5 text-xs font-medium uppercase tracking-wide ${tone}`}
        >
            {status.replace(/_/g, ' ')}
        </span>
    )
}
