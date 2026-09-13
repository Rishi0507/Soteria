import { useState } from 'react'
import {
    isInverted,
    THRESHOLD_MAX,
    THRESHOLD_MIN,
    THRESHOLD_STEP,
    type ConfigUpdate,
    type ContainmentConfig,
} from '../../api/containment'
import type { Notice } from './useThresholds'

type Props = {
    config: ContainmentConfig | null
    loading?: boolean
    loadError?: boolean
    saving?: boolean
    notice?: Notice | null
    onSave: (update: ConfigUpdate) => void
    onReload?: () => void
}

/**
 * ThresholdPanel is the confidence-cutoff surface: at or above the threshold a
 * hold is placed without a human, below it the incident waits in the review
 * queue.
 *
 * Server state arrives as props. The only state held here is the operator's
 * unsaved input — the two drafted numbers, who they say they are, and whether
 * they have acknowledged a pair that gives up the higher bar on whole-SKU holds.
 */
export function ThresholdPanel({
    config,
    loading,
    loadError,
    saving,
    notice,
    onSave,
    onReload,
}: Props) {
    // Actor lives above the form so it survives the form's remount on save: an
    // operator making two changes in a row should not retype who they are.
    const [actor, setActor] = useState('')

    if (loading) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-500">
                Loading thresholds&hellip;
            </section>
        )
    }

    if (loadError || !config) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-700">
                <p className="font-medium">We can&rsquo;t read the current thresholds.</p>
                <p className="mt-1 text-neutral-600">
                    Nothing has been changed. The containment service may be down, or this
                    console may be pointed at the wrong address.
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
            </section>
        )
    }

    return (
        <section className="rounded-xl border border-neutral-200 bg-white p-5">
            <header className="border-b border-neutral-100 pb-4">
                <h2 className="text-lg font-semibold text-neutral-900">
                    Auto-hold thresholds
                </h2>
                <p className="mt-1 text-sm text-neutral-600">
                    At or above these confidence scores a hold is placed without a human.
                    Below them the incident waits in the review queue.
                </p>
                <dl className="mt-3 flex flex-wrap gap-x-8 gap-y-2 text-sm">
                    <div>
                        <dt className="text-neutral-500">Current lot-level</dt>
                        <dd className="font-medium tabular-nums text-neutral-900">
                            {config.auto_hold_threshold}
                        </dd>
                    </div>
                    <div>
                        <dt className="text-neutral-500">Current whole-SKU</dt>
                        <dd className="font-medium tabular-nums text-neutral-900">
                            {config.sku_scope_threshold}
                        </dd>
                    </div>
                    <div>
                        <dt className="text-neutral-500">Last changed</dt>
                        <dd className="font-medium text-neutral-900">
                            {config.updated_by ?? 'unknown'}
                            <span className="ml-2 font-normal text-neutral-600">
                                {new Date(config.updated_at).toLocaleString()}
                            </span>
                        </dd>
                    </div>
                </dl>
            </header>

            <ThresholdForm
                // Remounting on the saved timestamp reseeds the drafts from what the
                // service actually stored, and clears a stale acknowledgement.
                key={config.updated_at}
                config={config}
                actor={actor}
                onActorChange={setActor}
                saving={Boolean(saving)}
                onSave={onSave}
            />

            {notice && (
                <p
                    role="status"
                    className={
                        'mt-4 rounded-lg px-3 py-2 text-sm ' +
                        (notice.kind === 'saved'
                            ? 'bg-emerald-50 text-emerald-900'
                            : 'bg-red-50 text-red-900')
                    }
                >
                    {notice.text}
                </p>
            )}

            <footer className="mt-4 border-t border-neutral-100 pt-3 text-xs text-neutral-500">
                Changes take effect immediately, with no redeploy. Only the current values
                are stored &mdash; this console cannot show what the thresholds were before.
            </footer>
        </section>
    )
}

function ThresholdForm({
    config,
    actor,
    onActorChange,
    saving,
    onSave,
}: {
    config: ContainmentConfig
    actor: string
    onActorChange: (value: string) => void
    saving: boolean
    onSave: (update: ConfigUpdate) => void
}) {
    const [autoHold, setAutoHold] = useState(String(config.auto_hold_threshold))
    const [skuScope, setSKUScope] = useState(String(config.sku_scope_threshold))
    const [acknowledged, setAcknowledged] = useState(false)
    const [problem, setProblem] = useState<string | null>(null)

    const autoValue = Number.parseFloat(autoHold)
    const skuValue = Number.parseFloat(skuScope)
    const inverted =
        Number.isFinite(autoValue) &&
        Number.isFinite(skuValue) &&
        isInverted(autoValue, skuValue)
    // Same gate, different sentence: equal gives up the higher bar, below
    // actively reverses it. The acknowledgement is identical either way.
    const equal = inverted && autoValue === skuValue

    // Editing either number invalidates an acknowledgement of the pair that was
    // on screen when the box was ticked.
    const change = (set: (value: string) => void) => (value: string) => {
        set(value)
        setAcknowledged(false)
        setProblem(null)
    }

    const submit = (e: React.FormEvent) => {
        e.preventDefault()

        if (
            !Number.isFinite(autoValue) ||
            autoValue < THRESHOLD_MIN ||
            autoValue > THRESHOLD_MAX
        ) {
            setProblem(
                `Lot-level threshold must be between ${THRESHOLD_MIN} and ${THRESHOLD_MAX}. The service rejects 0.`
            )
            return
        }
        if (
            !Number.isFinite(skuValue) ||
            skuValue < THRESHOLD_MIN ||
            skuValue > THRESHOLD_MAX
        ) {
            setProblem(
                `Whole-SKU threshold must be between ${THRESHOLD_MIN} and ${THRESHOLD_MAX}. The service reads 0 as "leave unchanged", so it is not offered here.`
            )
            return
        }
        if (!actor.trim()) {
            setProblem('Enter who is making this change.')
            return
        }
        if (inverted && !acknowledged) {
            setProblem('Acknowledge the threshold order before saving.')
            return
        }

        setProblem(null)
        onSave({
            auto_hold_threshold: autoValue,
            sku_scope_threshold: skuValue,
            actor: actor.trim(),
        })
    }

    return (
        <form className="pt-4" onSubmit={submit} noValidate>
            <div className="grid gap-4 sm:grid-cols-2">
                <Field
                    id="auto-hold"
                    label="Lot-level auto-hold"
                    hint="Holds only the named lots. Unaffected stock stays sellable."
                    value={autoHold}
                    onChange={change(setAutoHold)}
                    disabled={saving}
                />
                <Field
                    id="sku-scope"
                    label="Whole-SKU auto-hold"
                    hint="Used when no lot codes were recovered. Pulls the entire product."
                    value={skuScope}
                    onChange={change(setSKUScope)}
                    disabled={saving}
                />
            </div>

            {inverted && (
                <div
                    role="alert"
                    className="mt-4 rounded-lg border border-amber-400 bg-amber-50 p-4"
                >
                    <p className="text-sm font-semibold text-amber-900">
                        {equal
                            ? 'This makes pulling a whole product line no harder than pulling one lot.'
                            : 'This makes pulling a whole product line easier than pulling one lot.'}
                    </p>
                    {equal ? (
                        <p className="mt-1 text-sm text-amber-900">
                            The whole-SKU threshold ({skuScope}) is the same as the lot-level
                            threshold ({autoHold}). Any recall confident enough to hold a single
                            lot would also auto-hold the entire product whenever no lot codes
                            were recovered, destroying stock that was never implicated.
                            Removing a product line is meant to be the harder decision, not an
                            equally easy one.
                        </p>
                    ) : (
                        <p className="mt-1 text-sm text-amber-900">
                            The whole-SKU threshold ({skuScope}) is below the lot-level threshold
                            ({autoHold}). A recall that could not be narrowed to a lot would then
                            auto-hold the entire product at a confidence too low to hold a single
                            lot, destroying stock that was never implicated. The intended order is
                            the reverse: removing a product line should be the harder decision.
                        </p>
                    )}
                    <p className="mt-1 text-sm text-amber-900">
                        The containment service will accept this without complaint.
                    </p>
                    <label className="mt-3 flex items-start gap-2 text-sm text-amber-900">
                        <input
                            type="checkbox"
                            checked={acknowledged}
                            disabled={saving}
                            onChange={(e) => setAcknowledged(e.target.checked)}
                            className="mt-0.5"
                        />
                        <span>
                            I understand this gives up the higher bar on whole-product holds,
                            and I mean to set it this way.
                        </span>
                    </label>
                </div>
            )}

            <div className="mt-4">
                <label htmlFor="actor" className="block text-sm font-medium text-neutral-800">
                    Your name or operator ID
                </label>
                <input
                    id="actor"
                    value={actor}
                    disabled={saving}
                    onChange={(e) => {
                        onActorChange(e.target.value)
                        setProblem(null)
                    }}
                    placeholder="e.g. ops:dana"
                    className="mt-1 w-64 rounded-lg border border-neutral-300 px-3 py-2 text-sm disabled:bg-neutral-50"
                />
                <p className="mt-1 text-xs text-neutral-500">
                    Stored against the current values as who last changed them. It is not
                    checked against anything.
                </p>
            </div>

            {problem && (
                <p
                    role="alert"
                    className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-900"
                >
                    {problem}
                </p>
            )}

            <button
                type="submit"
                disabled={saving || (inverted && !acknowledged)}
                className="mt-4 rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800 disabled:opacity-50"
            >
                {saving ? 'Saving…' : 'Save thresholds'}
            </button>
        </form>
    )
}

function Field({
    id,
    label,
    hint,
    value,
    onChange,
    disabled,
}: {
    id: string
    label: string
    hint: string
    value: string
    onChange: (value: string) => void
    disabled: boolean
}) {
    return (
        <div>
            <label htmlFor={id} className="block text-sm font-medium text-neutral-800">
                {label}
            </label>
            <input
                id={id}
                type="number"
                inputMode="decimal"
                min={THRESHOLD_MIN}
                max={THRESHOLD_MAX}
                step={THRESHOLD_STEP}
                value={value}
                disabled={disabled}
                onChange={(e) => onChange(e.target.value)}
                className="mt-1 w-32 rounded-lg border border-neutral-300 px-3 py-2 text-sm tabular-nums disabled:bg-neutral-50"
            />
            <p className="mt-1 text-xs text-neutral-500">{hint}</p>
        </div>
    )
}
