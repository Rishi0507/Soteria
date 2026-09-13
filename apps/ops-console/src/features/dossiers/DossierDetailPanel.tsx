import { useState } from 'react'
import {
    counterCaveat,
    dossierPdfUrl,
    type Dossier,
    type Summary,
    type Verification,
} from '../../api/audit'
import { shortHash, when } from './format'
import type { DossierNotice } from './useDossier'
import { VolatileLedgerNote } from './VolatileLedgerNote'

type Props = {
    incidentId?: string
    dossier: Dossier | null
    state?: 'ready' | 'not-generated' | 'not-recorded' | 'error'
    loading?: boolean
    generating?: boolean
    notice?: DossierNotice | null
    verification?: Verification | null
    verifying?: boolean
    verifyError?: boolean
    onGenerate: () => void
    onReload?: () => void
}

/**
 * DossierDetailPanel shows one incident's record: whether the chain still
 * verifies, what the dossier says, and how to get the document itself.
 *
 * Server state arrives as props. The only state held here is whether the
 * reviewer has opened the regenerate control.
 */
export function DossierDetailPanel({
    incidentId,
    dossier,
    state = 'ready',
    loading,
    generating,
    notice,
    verification,
    verifying,
    verifyError,
    onGenerate,
    onReload,
}: Props) {
    if (loading) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-500">
                Loading the record&hellip;
            </section>
        )
    }

    if (state === 'error') {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-700">
                <p className="font-medium">We can&rsquo;t read this record.</p>
                <p className="mt-1 text-neutral-600">
                    This says nothing about whether it exists — only that the audit service
                    did not answer.
                </p>
                {onReload && <RetryButton onClick={onReload} />}
            </section>
        )
    }

    return (
        <section className="rounded-xl border border-neutral-200 bg-white p-5">
            <header className="border-b border-neutral-100 pb-4">
                <h2 className="text-lg font-semibold text-neutral-900">
                    {incidentId ?? dossier?.incident_id}
                </h2>
                <p className="mt-1 text-sm text-neutral-600">
                    The record of what happened during this incident, and the proof it has not
                    been edited since.
                </p>
            </header>

            <VerificationBlock
                verification={verification}
                verifying={verifying}
                failed={verifyError}
            />

            {state === 'not-recorded' && (
                <div className="pt-4">
                    <p className="text-sm font-medium text-neutral-900">
                        No events are recorded for this incident.
                    </p>
                    <VolatileLedgerNote context="missing" />
                    {onReload && <RetryButton onClick={onReload} />}
                </div>
            )}

            {state === 'not-generated' && (
                <div className="pt-4">
                    <p className="text-sm font-medium text-neutral-900">
                        No dossier has been built for this incident yet.
                    </p>
                    <p className="mt-1 text-sm text-neutral-600">
                        The events are on record and the chain above can be verified without a
                        document. A dossier is normally built automatically when containment
                        completes; this incident has not reached that point, or the generation
                        did not run.
                    </p>
                    <GenerateControl
                        mode="first"
                        generating={Boolean(generating)}
                        onGenerate={onGenerate}
                    />
                </div>
            )}

            {state === 'ready' && dossier && (
                <>
                    <DossierBody dossier={dossier} />
                    <GenerateControl
                        mode="again"
                        generating={Boolean(generating)}
                        onGenerate={onGenerate}
                    />
                </>
            )}

            {notice && (
                <p
                    role="status"
                    className={
                        'mt-4 rounded-lg px-3 py-2 text-sm ' +
                        (notice.kind === 'generated'
                            ? 'bg-emerald-50 text-emerald-900'
                            : 'bg-red-50 text-red-900')
                    }
                >
                    {notice.text}
                </p>
            )}
        </section>
    )
}

function RetryButton({ onClick }: { onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="mt-3 rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50"
        >
            Try again
        </button>
    )
}

// ------------------------------------------------------------- verification

/**
 * A broken chain is a successful request: 200 with verified:false. That response
 * omits content_hash and hash_algorithm, so neither is read without a guard.
 */
function VerificationBlock({
    verification,
    verifying,
    failed,
}: {
    verification?: Verification | null
    verifying?: boolean
    failed?: boolean
}) {
    if (verifying) {
        return (
            <p className="mt-4 rounded-lg bg-neutral-100 px-3 py-2 text-sm text-neutral-600">
                Recomputing the hash chain&hellip;
            </p>
        )
    }

    if (failed || !verification) {
        return (
            <p className="mt-4 rounded-lg bg-neutral-100 px-3 py-2 text-sm text-neutral-700">
                The chain could not be checked just now. That is not a finding either way —
                it means the request did not complete, not that anything is wrong with the
                record.
            </p>
        )
    }

    if (!verification.verified) {
        return (
            <div
                role="alert"
                className="mt-4 rounded-lg border border-red-300 bg-red-50 p-4 text-sm text-red-900"
            >
                <p className="font-semibold">This chain does not verify.</p>
                <p className="mt-1">
                    Recomputing {verification.events}{' '}
                    {verification.events === 1 ? 'record' : 'records'} found a record that does
                    not match what it should be. The service reports the first one it reaches
                    and stops there, so there may be others after it.
                </p>
                {verification.problem && (
                    <p className="mt-2 rounded bg-white/70 px-2 py-1 font-mono text-xs">
                        {verification.problem}
                    </p>
                )}
                <p className="mt-2">
                    Treat everything below as unproven until this is explained.
                </p>
            </div>
        )
    }

    return (
        <div className="mt-4 rounded-lg bg-emerald-50 p-3 text-sm text-emerald-900">
            <p className="font-medium">
                Chain verifies across {verification.events}{' '}
                {verification.events === 1 ? 'record' : 'records'}.
            </p>
            {verification.content_hash && (
                <p className="mt-1 font-mono text-xs">
                    head {shortHash(verification.content_hash)}
                    {verification.hash_algorithm ? ` · ${verification.hash_algorithm}` : ''}
                </p>
            )}
        </div>
    )
}

// ----------------------------------------------------------------- generate

/**
 * Generating is a write, and not an idempotent one: each call mints a new
 * dossier_id, re-renders the PDF and publishes another
 * audit.dossier.generated.v1 even when the chain has not moved. So it is behind
 * a disclosure that says exactly that, rather than a button someone can hit
 * twice without noticing.
 */
function GenerateControl({
    mode,
    generating,
    onGenerate,
}: {
    mode: 'first' | 'again'
    generating: boolean
    onGenerate: () => void
}) {
    const [open, setOpen] = useState(false)

    if (mode === 'first' && !open) {
        return (
            <button
                type="button"
                onClick={() => setOpen(true)}
                className="mt-3 rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800"
            >
                Build the dossier&hellip;
            </button>
        )
    }

    if (mode === 'again' && !open) {
        return (
            <button
                type="button"
                onClick={() => setOpen(true)}
                className="mt-4 text-sm font-medium text-neutral-700 underline underline-offset-2 hover:no-underline"
            >
                Generate a new copy&hellip;
            </button>
        )
    }

    return (
        <div className="mt-4 rounded-lg border border-neutral-300 bg-neutral-50 p-4">
            <p className="text-sm font-medium text-neutral-900">
                {mode === 'first' ? 'Build this dossier?' : 'Generate a new copy?'}
            </p>
            <p className="mt-1 text-sm text-neutral-700">
                This issues a new dossier id, re-renders the PDF, and publishes a
                dossier-generated event on the bus. It does that every time, even when the
                chain has not changed — so the evidence is the same, but the record of how
                many documents were produced is not.
            </p>
            {mode === 'again' && (
                <p className="mt-1 text-sm text-neutral-700">
                    The existing copy is replaced. There is no way to ask for the previous
                    one back.
                </p>
            )}
            <div className="mt-3 flex gap-2">
                <button
                    type="button"
                    onClick={onGenerate}
                    disabled={generating}
                    className="rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800 disabled:opacity-50"
                >
                    {generating ? 'Generating…' : 'Generate'}
                </button>
                <button
                    type="button"
                    onClick={() => setOpen(false)}
                    disabled={generating}
                    className="rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-white disabled:opacity-50"
                >
                    Cancel
                </button>
            </div>
        </div>
    )
}

// ------------------------------------------------------------------ dossier

function DossierBody({ dossier }: { dossier: Dossier }) {
    return (
        <div className="pt-4">
            <div className="flex flex-wrap items-center gap-3">
                <a
                    href={dossierPdfUrl(dossier.incident_id)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="rounded-lg border border-neutral-300 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50"
                >
                    Open the PDF
                </a>
                <span className="text-xs text-neutral-500">
                    Opens in a new tab — the service serves it inline rather than as a
                    download.
                </span>
            </div>

            <dl className="mt-4 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
                <Row label="Events recorded" value={String(dossier.event_count)} />
                <Row label="Generated" value={when(dossier.generated_at)} />
                <Row label="First event" value={when(dossier.opened_at)} />
                <Row label="Last event" value={when(dossier.closed_at)} />
                <Row
                    label="Content hash"
                    value={shortHash(dossier.content_hash)}
                    mono
                    hint={`The chain head, under ${dossier.hash_algorithm}. This is what identifies the evidence.`}
                />
                <Row
                    label="This generation"
                    value={dossier.dossier_id}
                    mono
                    hint="Identifies this rendering, not the incident. Reading this page can rebuild the dossier when new events have arrived, which issues a new id — so do not cite it as a fixed reference."
                />
            </dl>

            <TimestampBlock dossier={dossier} />
            {dossier.summary && <SummaryBlock summary={dossier.summary} />}
            {dossier.timeline && dossier.timeline.length > 0 && (
                <TimelineBlock timeline={dossier.timeline} />
            )}
        </div>
    )
}

function Row({
    label,
    value,
    hint,
    mono,
}: {
    label: string
    value: string
    hint?: string
    mono?: boolean
}) {
    return (
        <div>
            <dt className="text-neutral-500">{label}</dt>
            <dd
                className={
                    'font-medium text-neutral-900 ' + (mono ? 'break-all font-mono text-xs' : '')
                }
            >
                {value}
            </dd>
            {hint && <dd className="mt-0.5 text-xs text-neutral-500">{hint}</dd>}
        </div>
    )
}

/**
 * A proof and a note are mutually exclusive, and the note is the likely one: the
 * default authority is reached over plain HTTP and fails on any offline or
 * egress-restricted machine. The note is the service being honest about what it
 * could not obtain, so it is shown as a statement rather than an error.
 */
function TimestampBlock({ dossier }: { dossier: Dossier }) {
    if (dossier.timestamp_proof) {
        return (
            <div className="mt-4 rounded-lg bg-emerald-50 p-3 text-sm text-emerald-900">
                <p className="font-medium">
                    The content hash is anchored in time by a timestamp authority.
                </p>
                <p className="mt-1">
                    An RFC 3161 token over the hash above is included in the dossier. Verify it
                    with your own tools — this console does not check it, and should not be
                    taken as having done so.
                </p>
            </div>
        )
    }

    if (dossier.timestamp_note) {
        return (
            <div className="mt-4 rounded-lg border border-neutral-300 bg-neutral-50 p-3 text-sm text-neutral-800">
                <p className="font-medium">No timestamp was obtained.</p>
                <p className="mt-1">{dossier.timestamp_note}</p>
                <p className="mt-1 text-neutral-600">
                    The hash chain still shows the records have not been altered relative to
                    one another. What is missing is independent evidence of <em>when</em> they
                    existed.
                </p>
            </div>
        )
    }

    return (
        <div className="mt-4 rounded-lg bg-neutral-100 p-3 text-sm text-neutral-700">
            The dossier carries neither a timestamp token nor a note explaining its absence.
        </div>
    )
}

/**
 * Two counters cannot currently be anything but zero. They are marked rather
 * than omitted: dropping them would suggest the dossier has no such dimension,
 * when in fact it has one that nothing feeds yet — which is what a reader
 * relying on this document needs to know.
 */
function SummaryBlock({ summary }: { summary: Summary }) {
    const lists: [string, string[] | undefined][] = [
        ['Products', summary.products],
        ['Lots held', summary.lots_held],
        ['Sources', summary.sources],
    ]

    return (
        <div className="mt-5">
            <h3 className="text-sm font-semibold text-neutral-900">What happened</h3>

            <dl className="mt-2 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
                {summary.hazard && <Row label="Hazard" value={summary.hazard} />}
                {summary.classification && (
                    <Row label="Classification" value={summary.classification} />
                )}
                {summary.scope && <Row label="Scope" value={summary.scope} />}
                {summary.decision && (
                    <Row
                        label="Decision"
                        value={
                            summary.decision +
                            (summary.decided_by ? ` by ${summary.decided_by}` : '')
                        }
                    />
                )}
                {typeof summary.confidence === 'number' && (
                    <Row
                        label="Confidence vs threshold"
                        value={`${summary.confidence}${
                            typeof summary.threshold === 'number'
                                ? ` vs ${summary.threshold}`
                                : ''
                        }`}
                    />
                )}
            </dl>

            {lists.some(([, v]) => v && v.length > 0) && (
                <dl className="mt-3 space-y-2 text-sm">
                    {lists.map(([label, values]) =>
                        values && values.length > 0 ? (
                            <div key={label}>
                                <dt className="text-neutral-500">{label}</dt>
                                <dd className="text-neutral-900">{values.join(', ')}</dd>
                            </div>
                        ) : null
                    )}
                </dl>
            )}

            <ul className="mt-3 grid gap-2 sm:grid-cols-2">
                <Counter label="Units held" value={summary.units_held} />
                <Counter label="Units left sellable" value={summary.units_left_sellable} />
                <Counter label="Customers offered a rescue" value={summary.customers_offered} />
                <Counter label="Customers who answered" value={summary.customers_answered} />
                <Counter
                    label="Notifications sent"
                    value={summary.notifications_sent}
                    caveat={counterCaveat('notifications_sent')}
                />
                <Counter
                    label="Evasion flags"
                    value={summary.evasion_flags}
                    caveat={counterCaveat('evasion_flags')}
                />
            </ul>

            {summary.redacted_fields && summary.redacted_fields.length > 0 && (
                <p className="mt-3 text-xs text-neutral-600">
                    Withheld from the record: {summary.redacted_fields.join(', ')}. The hashes
                    are taken over the original payloads, so redaction does not weaken the
                    chain.
                </p>
            )}
        </div>
    )
}

function Counter({
    label,
    value,
    caveat,
}: {
    label: string
    value?: number
    caveat?: string | null
}) {
    return (
        <li className="rounded-lg border border-neutral-200 px-3 py-2">
            <p className="text-xs text-neutral-500">{label}</p>
            <p className="text-lg font-semibold tabular-nums text-neutral-900">
                {typeof value === 'number' ? value : '—'}
            </p>
            {caveat && (
                <p className="mt-0.5 text-xs text-amber-800">Not instrumented. {caveat}</p>
            )}
        </li>
    )
}

function TimelineBlock({ timeline }: { timeline: NonNullable<Dossier['timeline']> }) {
    return (
        <div className="mt-5">
            <h3 className="text-sm font-semibold text-neutral-900">Timeline</h3>
            <ol className="mt-2 space-y-2">
                {timeline.map((entry, i) => (
                    <li
                        key={`${entry.seq ?? i}-${entry.event ?? i}`}
                        className="rounded-lg border border-neutral-200 p-3 text-sm"
                    >
                        <div className="flex flex-wrap items-baseline gap-x-3">
                            <span className="font-mono text-xs text-neutral-500">
                                {entry.seq ?? '—'}
                            </span>
                            <span className="font-medium text-neutral-900">
                                {entry.event ?? 'unknown event'}
                            </span>
                            <span className="ml-auto text-xs text-neutral-500">
                                {when(entry.at)}
                            </span>
                        </div>
                        {entry.detail && (
                            <p className="mt-1 text-neutral-700">{entry.detail}</p>
                        )}
                        {entry.actor && (
                            <p className="mt-0.5 text-xs text-neutral-500">{entry.actor}</p>
                        )}
                    </li>
                ))}
            </ol>
        </div>
    )
}
