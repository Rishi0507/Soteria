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
            <section className="block text-ui text-ash-gray">
                Loading the action&hellip;
            </section>
        )
    }

    if (error || !action) {
        return (
            <section className="block text-ui text-silver-mist">
                <p className="font-normal">We can&rsquo;t read this action.</p>
                <p className="mt-1 text-ash-gray">Nothing has been decided.</p>
                {onReload && (
                    <button
                        type="button"
                        onClick={onReload}
                        className="mt-3 btn-ghost"
                    >
                        Try again
                    </button>
                )}
            </section>
        )
    }

    const pending = action.status === 'PENDING_REVIEW'

    return (
        <section className="block">
            <header className="border-b border-transparent pb-4">
                <div className="flex flex-wrap items-center gap-3">
                    <StatusBadge status={action.status} />
                    <h2 className="text-heading-2xs font-normal text-bone-white">
                        {action.hazard ?? 'Hazard not stated'}
                    </h2>
                </div>
                <dl className="mt-3 grid gap-x-8 gap-y-2 text-ui sm:grid-cols-2">
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
                <p className="mt-3 rounded-3xl px-3 py-2 text-ui text-bone-white">
                    <span className="font-normal">Why this is in the queue: </span>
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
            <dt className="text-ash-gray">{label}</dt>
            <dd className="font-normal text-bone-white">{value}</dd>
            {hint && <dd className="text-caption text-ash-gray">{hint}</dd>}
        </div>
    )
}

function Notice({ notice }: { notice: DecisionNotice }) {
    const tone =
        notice.kind === 'decided'
            ? 'chip chip-verdant'
            : notice.kind === 'partial' || notice.kind === 'conflict'
              ? 'chip chip-saffron'
              : 'chip chip-saffron'

    return (
        <p role="status" className={`mt-4 rounded-3xl px-3 py-2 text-ui${tone}`}>
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
            <h3 className="text-ui font-normal text-bone-white">
                {targets.length === 1 ? 'Product' : `Products (${targets.length})`}
            </h3>
            <ul className="mt-2 space-y-2">
                {targets.map((t) => {
                    const result = byGTIN.get(t.gtin)
                    return (
                        <li
                            key={t.gtin}
                            className="block text-ui"
                        >
                            <div className="flex flex-wrap items-baseline gap-x-3">
                                <span className="font-normal text-bone-white">
                                    {t.product_title ?? t.sku ?? t.gtin}
                                </span>
                                <span className="text-caption text-ash-gray">
                                    GTIN {t.gtin}
                                    {t.sku ? ` · SKU ${t.sku}` : ''}
                                </span>
                                <span className="ml-auto rounded-3xl px-2 py-0.5 text-caption font-normal text-silver-mist">
                                    {t.scope}
                                </span>
                            </div>

                            {isNarrowable(t) ? (
                                <p className="mt-1 text-silver-mist">
                                    Lots in scope:{' '}
                                    <span className="tabular-nums">
                                        {(t.lot_codes ?? []).join(', ')}
                                    </span>
                                </p>
                            ) : (
                                <p className="mt-1 rounded-3xl px-2 py-1 text-saffron-spark">
                                    <span className="font-normal">
                                        Whole product — this cannot be narrowed.
                                    </span>{' '}
                                    No lot codes were recovered for it, so confirming holds the
                                    entire product. The lot selection below has no effect here.
                                </p>
                            )}

                            {typeof t.confidence === 'number' && (
                                <p className="mt-1 text-caption text-ash-gray tabular-nums">
                                    match confidence {t.confidence}
                                </p>
                            )}

                            {result && <ResultLine result={result} />}
                        </li>
                    )
                })}
            </ul>

            {status === 'PENDING_REVIEW' && (
                <p className="mt-2 text-caption text-ash-gray">
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
            ? 'chip chip-verdant'
            : result.status === 'FAILED'
              ? 'chip chip-saffron'
              : 'chip'

    return (
        <div className={`mt-2 rounded-3xl px-2 py-1 text-ui${tone}`}>
            <span className="font-normal">{result.status}</span>
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
                <span className="ml-2 text-caption opacity-75">{result.platform_ref}</span>
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
        <div className="mt-5 border-t border-transparent pt-4">
            {narrowable.length > 0 ? (
                <fieldset disabled={deciding}>
                    <legend className="text-ui font-normal text-bone-white">
                        Lots to hold
                    </legend>
                    <p className="mt-1 text-ui text-ash-gray">
                        All lots are selected. Unselect one to leave it on sale. You can only
                        narrow what was already in scope — there is no way to add a lot here.
                    </p>
                    {narrowable.length > 1 && (
                        <p className="mt-1 rounded-3xl px-3 py-2 text-ui text-bone-white">
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
                                    <label className="flex items-start gap-2 text-ui text-bone-white">
                                        <input
                                            type="checkbox"
                                            checked={isOn}
                                            onChange={() => toggle(code)}
                                            className="mt-1"
                                        />
                                        <span>
                                            <span className="font-normal tabular-nums">{code}</span>
                                            {carriers.length > 1 && (
                                                <span className="ml-2 text-caption text-saffron-spark">
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
                            className="mt-3 rounded-3xl border border-saffron-spark/60 p-3 text-ui text-saffron-spark"
                        >
                            <p className="font-normal">
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
                        <p className="mt-3 text-ui text-saffron-spark">
                            {action.targets.length - narrowable.length} of{' '}
                            {action.targets.length} products have no lot codes and will be held
                            whole whatever is selected here.
                        </p>
                    )}
                </fieldset>
            ) : (
                <p className="rounded-3xl px-3 py-2 text-ui text-saffron-spark">
                    No lot codes were recovered on any product here, so nothing can be
                    narrowed. Confirming holds every product on this action in full.
                </p>
            )}

            <div className="mt-4">
                <label htmlFor="actor" className="block text-ui font-normal text-bone-white">
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
                    className="mt-1 w-64 btn-ghost"
                />
                <p className="mt-1 text-caption text-ash-gray">
                    Recorded against the decision. It is not checked against anything.
                </p>
            </div>

            <div className="mt-4 grid gap-4 lg:grid-cols-2">
                <div className="block">
                    <h3 className="text-ui font-normal text-bone-white">Confirm the hold</h3>
                    <label htmlFor="note" className="mt-2 block text-ui text-silver-mist">
                        Note (optional)
                    </label>
                    <input
                        id="note"
                        value={note}
                        disabled={deciding}
                        onChange={(e) => setNote(e.target.value)}
                        placeholder="e.g. checked against the FDA notice"
                        className="mt-1 w-full btn-ghost"
                    />
                    <button
                        type="button"
                        onClick={confirm}
                        disabled={deciding || emptied.length > 0}
                        className="mt-3 btn-primary"
                    >
                        {deciding ? 'Working…' : 'Confirm hold'}
                    </button>
                </div>

                <div className="block">
                    <h3 className="text-ui font-normal text-bone-white">Reject</h3>
                    <p className="mt-1 text-caption text-ash-gray">
                        Nothing is held. This cannot be undone — there is no way to reopen a
                        rejected action.
                    </p>
                    <label htmlFor="reason" className="mt-2 block text-ui text-silver-mist">
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
                        className="mt-1 w-full btn-ghost"
                    />
                    <button
                        type="button"
                        onClick={reject}
                        disabled={deciding}
                        className="mt-3 btn-ghost"
                    >
                        {deciding ? 'Working…' : 'Reject'}
                    </button>
                </div>
            </div>

            {problem && (
                <p role="alert" className="mt-4 rounded-3xl px-3 py-2 text-ui text-saffron-spark">
                    {problem}
                </p>
            )}

            {narrowed && emptied.length === 0 && (
                <p className="mt-3 text-caption text-ash-gray">
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
        <div className="mt-5 border-t border-transparent pt-4 text-ui">
            <p className="text-bone-white">
                <span className="font-normal">
                    {rejected ? 'Rejected' : 'Decided'}
                </span>
                {action.actor ? ` by ${action.actor}` : ''}
                {action.decided_at
                    ? ` on ${new Date(action.decided_at).toLocaleString()}`
                    : ''}
                .
            </p>
            {action.note && (
                <p className="mt-1 text-silver-mist">
                    {/* On a rejection the service stores the reviewer's reason in
                        `note`; `reason` keeps the auto-generated queueing text. */}
                    <span className="font-normal">
                        {rejected ? 'Reason given: ' : 'Note: '}
                    </span>
                    {action.note}
                </p>
            )}
            {!rejected && (action.results?.length ?? 0) === 0 && (
                <p className="mt-1 text-ash-gray">
                    No per-target results were returned.
                </p>
            )}
        </div>
    )
}
