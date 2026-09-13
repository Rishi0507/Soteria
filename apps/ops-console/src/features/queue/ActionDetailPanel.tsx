import { useState } from 'react'
import {
    isNarrowable,
    keptLots,
    lotUnion,
    targetsUnaffectedBySelection,
    targetsWithLot,
    type ConfirmRequest,
    type ContainmentAction,
    type ContainmentResult,
    type ContainmentTarget,
    type RejectRequest,
} from '../../api/containment'
import type { DecisionNotice } from './useActionReview'
import { StatusBadge } from './ReviewQueuePanel'

type Props = {
    action: ContainmentAction | null
    loading?: boolean
    error?: boolean
    deciding?: boolean
    notice?: DecisionNotice | null
    onConfirm: (body: ConfirmRequest) => void
    onReject: (body: RejectRequest) => void
    onReload?: () => void
}

/**
 * ActionDetailPanel is where a reviewer decides. Server state arrives as props;
 * the only state held here is the unsaved decision — who they are, which lots
 * they intend to keep holding, and the note or reason they are writing.
 */
export function ActionDetailPanel({
    action,
    loading,
    error,
    deciding,
    notice,
    onConfirm,
    onReject,
    onReload,
}: Props) {
    if (loading) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-500">
                Loading the action&hellip;
            </section>
        )
    }

    if (error || !action) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-700">
                <p className="font-medium">We can&rsquo;t read this action.</p>
                <p className="mt-1 text-neutral-600">Nothing has been decided.</p>
                {onReload && (
                    <button
                        type="button"
                        onClick={onReload}
                        className="mt-3 rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50"
                    >
                        Try again
                    </button>
                )}
            </section>
        )
    }

    const pending = action.status === 'PENDING_REVIEW'

    return (
        <section className="rounded-xl border border-neutral-200 bg-white p-5">
            <header className="border-b border-neutral-100 pb-4">
                <div className="flex flex-wrap items-center gap-3">
                    <StatusBadge status={action.status} />
                    <h2 className="text-lg font-semibold text-neutral-900">
                        {action.hazard ?? 'Hazard not stated'}
                    </h2>
                </div>
                <dl className="mt-3 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
                    <Row label="Incident" value={action.incident_id} />
                    <Row label="Action" value={action.action_id} />
                    <Row
                        label="Confidence vs threshold"
                        value={`${action.confidence} vs ${action.threshold}`}
                        hint="The cutoff in force when this was decided, not the current setting."
                    />
                    <Row
                        label="Created"
                        value={new Date(action.created_at).toLocaleString()}
                    />
                </dl>
                <p className="mt-3 rounded-lg bg-neutral-100 px-3 py-2 text-sm text-neutral-800">
                    <span className="font-medium">Why this is in the queue: </span>
                    {action.reason ?? 'No queueing reason was recorded.'}
                </p>
            </header>

            <TargetList
                targets={action.targets}
                results={action.results}
                status={action.status}
            />

            {pending ? (
                <DecisionForm
                    key={action.action_id}
                    action={action}
                    deciding={Boolean(deciding)}
                    onConfirm={onConfirm}
                    onReject={onReject}
                />
            ) : (
                <Outcome action={action} />
            )}

            {notice && <Notice notice={notice} />}
        </section>
    )
}

function Row({
    label,
    value,
    hint,
}: {
    label: string
    value: string
    hint?: string
}) {
    return (
        <div>
            <dt className="text-neutral-500">{label}</dt>
            <dd className="font-medium text-neutral-900">{value}</dd>
            {hint && <dd className="text-xs text-neutral-500">{hint}</dd>}
        </div>
    )
}

function Notice({ notice }: { notice: DecisionNotice }) {
    const tone =
        notice.kind === 'decided'
            ? 'bg-emerald-50 text-emerald-900'
            : notice.kind === 'partial' || notice.kind === 'conflict'
              ? 'bg-amber-50 text-amber-900'
              : 'bg-red-50 text-red-900'

    return (
        <p role="status" className={`mt-4 rounded-lg px-3 py-2 text-sm ${tone}`}>
            {notice.text}
        </p>
    )
}

// ------------------------------------------------------------------ targets

function TargetList({
    targets,
    results,
    status,
}: {
    targets: ContainmentTarget[]
    results?: ContainmentResult[]
    status: ContainmentAction['status']
}) {
    const byGTIN = new Map((results ?? []).map((r) => [r.gtin, r]))

    return (
        <div className="pt-4">
            <h3 className="text-sm font-semibold text-neutral-900">
                {targets.length === 1 ? 'Product' : `Products (${targets.length})`}
            </h3>
            <ul className="mt-2 space-y-2">
                {targets.map((t) => {
                    const result = byGTIN.get(t.gtin)
                    return (
                        <li
                            key={t.gtin}
                            className="rounded-lg border border-neutral-200 p-3 text-sm"
                        >
                            <div className="flex flex-wrap items-baseline gap-x-3">
                                <span className="font-medium text-neutral-900">
                                    {t.product_title ?? t.sku ?? t.gtin}
                                </span>
                                <span className="text-xs text-neutral-500">
                                    GTIN {t.gtin}
                                    {t.sku ? ` · SKU ${t.sku}` : ''}
                                </span>
                                <span className="ml-auto rounded bg-neutral-100 px-2 py-0.5 text-xs font-medium text-neutral-700">
                                    {t.scope}
                                </span>
                            </div>

                            {isNarrowable(t) ? (
                                <p className="mt-1 text-neutral-700">
                                    Lots in scope:{' '}
                                    <span className="tabular-nums">
                                        {(t.lot_codes ?? []).join(', ')}
                                    </span>
                                </p>
                            ) : (
                                <p className="mt-1 rounded bg-amber-50 px-2 py-1 text-amber-900">
                                    <span className="font-medium">
                                        Whole product — this cannot be narrowed.
                                    </span>{' '}
                                    No lot codes were recovered for it, so confirming holds the
                                    entire product. The lot selection below has no effect here.
                                </p>
                            )}

                            {typeof t.confidence === 'number' && (
                                <p className="mt-1 text-xs text-neutral-500 tabular-nums">
                                    match confidence {t.confidence}
                                </p>
                            )}

                            {result && <ResultLine result={result} />}
                        </li>
                    )
                })}
            </ul>

            {status === 'PENDING_REVIEW' && (
                <p className="mt-2 text-xs text-neutral-500">
                    How many units this would hold is not known until the hold runs — the
                    service returns unit counts only in the result of a confirmation.
                </p>
            )}
        </div>
    )
}

function ResultLine({ result }: { result: ContainmentResult }) {
    const tone =
        result.status === 'HELD'
            ? 'bg-emerald-50 text-emerald-900'
            : result.status === 'FAILED'
              ? 'bg-red-50 text-red-900'
              : 'bg-neutral-100 text-neutral-800'

    return (
        <div className={`mt-2 rounded px-2 py-1 text-sm ${tone}`}>
            <span className="font-medium">{result.status}</span>
            {result.status === 'HELD' && (
                <span className="ml-2 tabular-nums">
                    {result.units_held ?? 0} units held
                    {typeof result.units_left_sellable === 'number'
                        ? `, ${result.units_left_sellable} left sellable`
                        : ''}
                </span>
            )}
            {result.error && <span className="ml-2">{result.error}</span>}
            {result.platform_ref && (
                <span className="ml-2 text-xs opacity-75">{result.platform_ref}</span>
            )}
        </div>
    )
}

// ----------------------------------------------------------------- decision

function DecisionForm({
    action,
    deciding,
    onConfirm,
    onReject,
}: {
    action: ContainmentAction
    deciding: boolean
    onConfirm: (body: ConfirmRequest) => void
    onReject: (body: RejectRequest) => void
}) {
    const union = lotUnion(action.targets)
    const [selected, setSelected] = useState<string[]>(union)
    const [actor, setActor] = useState('')
    const [note, setNote] = useState('')
    const [reason, setReason] = useState('')
    const [problem, setProblem] = useState<string | null>(null)

    const narrowable = action.targets.filter(isNarrowable)
    const emptied = targetsUnaffectedBySelection(action.targets, selected)
    const narrowed = selected.length < union.length

    const toggle = (code: string) => {
        const key = code.trim().toUpperCase()
        setProblem(null)
        setSelected((prev) =>
            prev.some((c) => c.trim().toUpperCase() === key)
                ? prev.filter((c) => c.trim().toUpperCase() !== key)
                : [...prev, code]
        )
    }

    const confirm = () => {
        if (!actor.trim()) {
            setProblem('Enter who is making this decision.')
            return
        }
        if (emptied.length > 0) {
            setProblem(
                'Select at least one lot on every product below, or this would hold more, not less.'
            )
            return
        }
        setProblem(null)
        onConfirm({
            actor: actor.trim(),
            note: note.trim() || undefined,
            // Sending the full set is identical to sending nothing, because the
            // service intersects. Omitting it keeps the request honest about
            // whether the reviewer actually narrowed anything.
            lot_codes_override: narrowed ? selected : undefined,
        })
    }

    const reject = () => {
        if (!actor.trim()) {
            setProblem('Enter who is making this decision.')
            return
        }
        if (!reason.trim()) {
            setProblem('A rejection needs a reason.')
            return
        }
        setProblem(null)
        onReject({ actor: actor.trim(), reason: reason.trim() })
    }

    return (
        <div className="mt-5 border-t border-neutral-100 pt-4">
            {narrowable.length > 0 ? (
                <fieldset disabled={deciding}>
                    <legend className="text-sm font-semibold text-neutral-900">
                        Lots to hold
                    </legend>
                    <p className="mt-1 text-sm text-neutral-600">
                        All lots are selected. Unselect one to leave it on sale. You can only
                        narrow what was already in scope — there is no way to add a lot here.
                    </p>
                    {narrowable.length > 1 && (
                        <p className="mt-1 rounded-lg bg-neutral-100 px-3 py-2 text-sm text-neutral-800">
                            This one list applies to every product on this action. The service
                            takes no per-product instruction, so unselecting a code removes it
                            from every product that carries it.
                        </p>
                    )}

                    <ul className="mt-3 space-y-1">
                        {union.map((code) => {
                            const carriers = targetsWithLot(action.targets, code)
                            const isOn = selected.some(
                                (c) => c.trim().toUpperCase() === code.trim().toUpperCase()
                            )
                            return (
                                <li key={code}>
                                    <label className="flex items-start gap-2 text-sm text-neutral-800">
                                        <input
                                            type="checkbox"
                                            checked={isOn}
                                            onChange={() => toggle(code)}
                                            className="mt-1"
                                        />
                                        <span>
                                            <span className="font-medium tabular-nums">{code}</span>
                                            {carriers.length > 1 && (
                                                <span className="ml-2 text-xs text-amber-800">
                                                    on {carriers.length} products:{' '}
                                                    {carriers
                                                        .map((t) => t.product_title ?? t.gtin)
                                                        .join(', ')}
                                                </span>
                                            )}
                                        </span>
                                    </label>
                                </li>
                            )
                        })}
                    </ul>

                    {emptied.length > 0 && (
                        <div
                            role="alert"
                            className="mt-3 rounded-lg border border-red-300 bg-red-50 p-3 text-sm text-red-900"
                        >
                            <p className="font-semibold">
                                This selection would hold more, not less.
                            </p>
                            <p className="mt-1">
                                No lot is selected on{' '}
                                {emptied.map((t) => t.product_title ?? t.gtin).join(', ')}. The
                                service ignores a lot list that matches nothing on a product and
                                holds every lot that product had, answering as though the
                                narrowing worked. Select at least one lot there.
                            </p>
                            <p className="mt-1">
                                To hold nothing at all, reject this action instead.
                            </p>
                        </div>
                    )}

                    {narrowable.length < action.targets.length && (
                        <p className="mt-3 text-sm text-amber-900">
                            {action.targets.length - narrowable.length} of{' '}
                            {action.targets.length} products have no lot codes and will be held
                            whole whatever is selected here.
                        </p>
                    )}
                </fieldset>
            ) : (
                <p className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
                    No lot codes were recovered on any product here, so nothing can be
                    narrowed. Confirming holds every product on this action in full.
                </p>
            )}

            <div className="mt-4">
                <label htmlFor="actor" className="block text-sm font-medium text-neutral-800">
                    Your name or operator ID
                </label>
                <input
                    id="actor"
                    value={actor}
                    disabled={deciding}
                    onChange={(e) => {
                        setActor(e.target.value)
                        setProblem(null)
                    }}
                    placeholder="e.g. ops:dana"
                    className="mt-1 w-64 rounded-lg border border-neutral-300 px-3 py-2 text-sm disabled:bg-neutral-50"
                />
                <p className="mt-1 text-xs text-neutral-500">
                    Recorded against the decision. It is not checked against anything.
                </p>
            </div>

            <div className="mt-4 grid gap-4 lg:grid-cols-2">
                <div className="rounded-lg border border-neutral-200 p-4">
                    <h3 className="text-sm font-semibold text-neutral-900">Confirm the hold</h3>
                    <label htmlFor="note" className="mt-2 block text-sm text-neutral-700">
                        Note (optional)
                    </label>
                    <input
                        id="note"
                        value={note}
                        disabled={deciding}
                        onChange={(e) => setNote(e.target.value)}
                        placeholder="e.g. checked against the FDA notice"
                        className="mt-1 w-full rounded-lg border border-neutral-300 px-3 py-2 text-sm disabled:bg-neutral-50"
                    />
                    <button
                        type="button"
                        onClick={confirm}
                        disabled={deciding || emptied.length > 0}
                        className="mt-3 rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800 disabled:opacity-50"
                    >
                        {deciding ? 'Working…' : 'Confirm hold'}
                    </button>
                </div>

                <div className="rounded-lg border border-neutral-200 p-4">
                    <h3 className="text-sm font-semibold text-neutral-900">Reject</h3>
                    <p className="mt-1 text-xs text-neutral-600">
                        Nothing is held. This cannot be undone — there is no way to reopen a
                        rejected action.
                    </p>
                    <label htmlFor="reason" className="mt-2 block text-sm text-neutral-700">
                        Reason (required)
                    </label>
                    <input
                        id="reason"
                        value={reason}
                        disabled={deciding}
                        onChange={(e) => {
                            setReason(e.target.value)
                            setProblem(null)
                        }}
                        placeholder="e.g. notice refers to a different brand"
                        className="mt-1 w-full rounded-lg border border-neutral-300 px-3 py-2 text-sm disabled:bg-neutral-50"
                    />
                    <button
                        type="button"
                        onClick={reject}
                        disabled={deciding}
                        className="mt-3 rounded-lg border border-neutral-400 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50 disabled:opacity-50"
                    >
                        {deciding ? 'Working…' : 'Reject'}
                    </button>
                </div>
            </div>

            {problem && (
                <p role="alert" className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-900">
                    {problem}
                </p>
            )}

            {narrowed && emptied.length === 0 && (
                <p className="mt-3 text-xs text-neutral-600">
                    Confirming will hold:{' '}
                    {narrowable
                        .map(
                            (t) =>
                                `${t.product_title ?? t.gtin} (${keptLots(t, selected).join(', ')})`
                        )
                        .join('; ')}
                    .
                </p>
            )}
        </div>
    )
}

// ------------------------------------------------------------------ outcome

function Outcome({ action }: { action: ContainmentAction }) {
    const rejected = action.status === 'HUMAN_REJECTED'

    return (
        <div className="mt-5 border-t border-neutral-100 pt-4 text-sm">
            <p className="text-neutral-800">
                <span className="font-medium">
                    {rejected ? 'Rejected' : 'Decided'}
                </span>
                {action.actor ? ` by ${action.actor}` : ''}
                {action.decided_at
                    ? ` on ${new Date(action.decided_at).toLocaleString()}`
                    : ''}
                .
            </p>
            {action.note && (
                <p className="mt-1 text-neutral-700">
                    {/* On a rejection the service stores the reviewer's reason in
                        `note`; `reason` keeps the auto-generated queueing text. */}
                    <span className="font-medium">
                        {rejected ? 'Reason given: ' : 'Note: '}
                    </span>
                    {action.note}
                </p>
            )}
            {!rejected && (action.results?.length ?? 0) === 0 && (
                <p className="mt-1 text-neutral-600">
                    No per-target results were returned.
                </p>
            )}
        </div>
    )
}
