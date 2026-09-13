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
        <section className="block">
            <header className="flex flex-wrap items-center justify-between gap-3 border-b border-transparent p-4">
                <div>
                    <h2 className="text-heading-2xs font-normal text-bone-white">Review queue</h2>
                    <p className="mt-1 text-ui text-ash-gray">
                        Containment decisions that scored below the threshold in force when
                        they were made, and the history of those already decided.
                    </p>
                </div>
                <label className="text-ui">
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
                        className="btn-ghost"
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
                <p className="p-5 text-ui text-ash-gray">Loading the queue&hellip;</p>
            )}

            {!loading && error && (
                <div className="p-5 text-ui text-silver-mist">
                    <p className="font-normal">We can&rsquo;t read the review queue.</p>
                    <p className="mt-1 text-ash-gray">
                        Nothing has been decided. The containment service may be down.
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

            {!loading && !error && actions.length === 0 && (
                <p className="p-5 text-ui text-ash-gray">
                    {status
                        ? `Nothing with status ${status.replace(/_/g, ' ').toLowerCase()}.`
                        : 'Nothing in the queue. Every resolved recall so far cleared its threshold or has already been decided.'}
                </p>
            )}

            {!loading && !error && actions.length > 0 && (
                <ul className="divide-y divide-white/10">
                    {actions.map((action) => (
                        <li key={action.action_id}>
                            <button
                                type="button"
                                onClick={() => onSelect(action.action_id)}
                                aria-current={action.action_id === selectedId}
                                className={
                                    'w-full px-4 py-3 text-left hover:bg-white/5 ' +
                                    (action.action_id === selectedId ? 'bg-white/5' : '')
                                }
                            >
                                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                                    <StatusBadge status={action.status} />
                                    <span className="font-normal text-bone-white">
                                        {action.hazard ?? 'Hazard not stated'}
                                    </span>
                                    <span className="text-caption text-ash-gray">
                                        {action.incident_id}
                                    </span>
                                    <span className="ml-auto text-caption text-ash-gray">
                                        {ageLabel(action.created_at)}
                                    </span>
                                </div>

                                <p className="mt-1 text-ui text-silver-mist">
                                    {action.reason ?? 'No queueing reason recorded.'}
                                </p>

                                <p className="mt-1 text-caption text-ash-gray">
                                    <span className="tabular-nums">
                                        confidence {action.confidence} vs threshold{' '}
                                        {action.threshold}
                                    </span>
                                    <span className="ml-2 text-ash-gray">
                                        (the cutoff in force when this was decided)
                                    </span>
                                </p>

                                <p className="mt-1 text-caption text-ash-gray">
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
            ? 'chip chip-saffron'
            : status === 'HUMAN_REJECTED'
              ? 'chip'
              : status === 'FAILED'
                ? 'chip chip-saffron'
                : 'chip chip-verdant'

    return (
        <span
            className={`rounded-full px-2 py-0.5 text-caption font-normal uppercase tracking-wide${tone}`}
        >
            {status.replace(/_/g, ' ')}
        </span>
    )
}
