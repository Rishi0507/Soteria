import type { components, operations } from './audit.types'

export type Dossier = components['schemas']['Dossier']
export type Summary = components['schemas']['Summary']
export type TimelineEntry = components['schemas']['TimelineEntry']
export type ChainRecord = components['schemas']['Record']
export type Chain = components['schemas']['Chain']

/** Rows of the archive list. The spec declares this inline, not as a named schema. */
export type ArchiveRow =
    operations['listDossiers']['responses'][200]['content']['application/json']['items'][number]

/**
 * The verify result.
 *
 * A broken chain answers 200 with verified:false and OMITS content_hash and
 * hash_algorithm — they are only present on success. Both are optional here for
 * that reason, and neither may be read without a guard.
 */
export type Verification =
    operations['verifyChain']['responses'][200]['content']['application/json']

const BASE = import.meta.env.VITE_AUDIT_API_URL

/**
 * 404 from this service means one of two things it cannot tell apart: the
 * incident was never recorded, or the ledger holding it was lost. See
 * LEDGER_IS_VOLATILE.
 */
export class NotRecordedError extends Error {}

/** The incident has a chain, but nobody has generated a dossier for it yet. */
export class NotGeneratedError extends Error {}

export class AuditServiceError extends Error {}

/**
 * The ledger is an in-memory map, rebuilt empty on every start
 * (services/audit-proof-service/ledger/ledger.go). Nothing in any response
 * distinguishes "nothing has happened yet" from "the record was lost": an empty
 * list is byte-identical either way, and a 404 on an incident that verified
 * yesterday is byte-identical to a 404 on an id that never existed.
 *
 * For the service whose entire claim is that the record is complete, that has to
 * be said out loud rather than inferred from an empty screen.
 */
export const LEDGER_IS_VOLATILE = true

export async function listDossiers(): Promise<ArchiveRow[]> {
    const res = await fetch(`${BASE}/v1/dossiers`)
    if (!res.ok) {
        throw new AuditServiceError(`Archive lookup failed: ${res.status}`)
    }
    const body = (await res.json()) as { items?: ArchiveRow[] | null }
    return body.items ?? []
}

/**
 * A 404 here is the ordinary "not generated yet" case, not a failure: the list
 * is built from the ledger, so a row exists for every recorded incident whether
 * or not a dossier was ever built for it.
 */
export async function getDossier(incidentId: string): Promise<Dossier> {
    const res = await fetch(`${BASE}/v1/dossiers/${encodeURIComponent(incidentId)}`)
    if (res.status === 404) {
        throw new NotGeneratedError('No dossier has been generated for this incident.')
    }
    if (!res.ok) {
        throw new AuditServiceError(`Dossier lookup failed: ${res.status}`)
    }
    return res.json()
}

/**
 * Generating is a write, and not an idempotent one: every call mints a new
 * dossier_id, re-renders the PDF and publishes another
 * audit.dossier.generated.v1, even when the chain has not moved. Callers are
 * expected to make the user ask for it explicitly.
 */
export async function generateDossier(incidentId: string): Promise<Dossier> {
    const res = await fetch(`${BASE}/v1/dossiers/${encodeURIComponent(incidentId)}`, {
        method: 'POST',
    })
    if (res.status === 404) {
        throw new NotRecordedError('No events are recorded for this incident.')
    }
    if (!res.ok) {
        throw new AuditServiceError(`Generation failed: ${res.status}`)
    }
    return res.json()
}

export async function verifyChain(incidentId: string): Promise<Verification> {
    const res = await fetch(
        `${BASE}/v1/dossiers/${encodeURIComponent(incidentId)}/verify`
    )
    if (res.status === 404) {
        throw new NotRecordedError('No events are recorded for this incident.')
    }
    if (!res.ok) {
        throw new AuditServiceError(`Verification failed: ${res.status}`)
    }
    // A failed verification is a successful request: 200 with verified:false.
    return res.json()
}

export async function getChain(incidentId: string): Promise<Chain> {
    const res = await fetch(
        `${BASE}/v1/dossiers/${encodeURIComponent(incidentId)}/chain`
    )
    if (res.status === 404) {
        throw new NotRecordedError('No events are recorded for this incident.')
    }
    if (!res.ok) {
        throw new AuditServiceError(`Chain lookup failed: ${res.status}`)
    }
    return res.json()
}

/**
 * The PDF is served by appending `.pdf` to the incident id, which the handler
 * parses with strings.HasSuffix — the suffix is part of the path segment, not a
 * query or an Accept header. Case-sensitive: `.PDF` falls through to the JSON
 * branch and 404s.
 */
export function dossierPdfUrl(incidentId: string): string {
    return `${BASE}/v1/dossiers/${encodeURIComponent(incidentId)}.pdf`
}

/**
 * Counters that cannot currently be anything but zero, with the reason.
 *
 * A bare "0" reads as "we looked and found none". For these two nothing ever
 * looks, so the dossier must not present them as findings.
 */
export function counterCaveat(field: keyof Summary): string | null {
    if (field === 'evasion_flags') {
        return 'No marketplace monitoring exists yet, so nothing can raise this.'
    }
    if (field === 'notifications_sent') {
        return 'Only counted when the services run against a real broker.'
    }
    return null
}
