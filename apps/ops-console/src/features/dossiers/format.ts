/**
 * Formatting helpers for the archive.
 *
 * Kept out of the component files so Fast Refresh keeps working: a module that
 * exports anything but components loses it.
 */

/** Chain heads are 64 hex characters; only the ends are useful on screen. */
export function shortHash(hash?: string): string {
    if (!hash) return ''
    return hash.length <= 16 ? hash : `${hash.slice(0, 8)}…${hash.slice(-8)}`
}

/** A date-time the service may simply not have sent. */
export function when(value?: string): string {
    if (!value) return 'not recorded'
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}
