/**
 * Queue age, coarsened to the unit that matters at a glance.
 *
 * Lives outside the component file so Fast Refresh keeps working: a module that
 * exports anything but components loses it.
 */
export function ageLabel(createdAt: string, now: Date = new Date()): string {
    const ms = now.getTime() - new Date(createdAt).getTime()
    if (!Number.isFinite(ms) || ms < 0) return 'just now'
    const minutes = Math.floor(ms / 60000)
    if (minutes < 1) return 'just now'
    if (minutes < 60) return `${minutes}m in queue`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h in queue`
    return `${Math.floor(hours / 24)}d in queue`
}
