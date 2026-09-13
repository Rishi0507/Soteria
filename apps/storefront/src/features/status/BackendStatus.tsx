import { useEffect, useState } from 'react'

type Health = { status?: string; service?: string; bus?: string }

const SERVICES = [
    { label: 'resolution', url: import.meta.env.VITE_RESOLUTION_API_URL },
    { label: 'rescue', url: import.meta.env.VITE_RESCUE_API_URL },
]

type Probe = { label: string; up: boolean | null; detail: string }

/**
 * BackendStatus probes each service's /healthz and shows the result.
 *
 * It exists so nobody has to wonder whether the page is showing live data or a
 * fixture: if a dot is red, every verdict on screen came from a service that is
 * not answering, and should be trusted accordingly. Stub data that looks live is
 * the failure mode this whole product exists to prevent, so the storefront says
 * out loud what it is talking to.
 */
export function BackendStatus() {
    const [probes, setProbes] = useState<Probe[]>(
        SERVICES.map((s) => ({ label: s.label, up: null, detail: 'checking' }))
    )

    useEffect(() => {
        let cancelled = false

        const check = async () => {
            const next = await Promise.all(
                SERVICES.map(async ({ label, url }) => {
                    if (!url) {
                        return { label, up: false, detail: 'no URL configured' }
                    }
                    try {
                        const res = await fetch(`${url}/healthz`)
                        if (!res.ok) {
                            return { label, up: false, detail: `HTTP ${res.status}` }
                        }
                        const body = (await res.json()) as Health
                        return {
                            label,
                            up: body.status === 'ok',
                            detail: body.bus ? `bus ${body.bus}` : (body.status ?? 'unknown'),
                        }
                    } catch {
                        return { label, up: false, detail: 'unreachable' }
                    }
                })
            )
            if (!cancelled) setProbes(next)
        }

        check()
        const timer = setInterval(check, 15000)
        return () => {
            cancelled = true
            clearInterval(timer)
        }
    }, [])

    return (
        <ul className="hidden items-center gap-3 text-caption text-pebble lg:flex" title="Recall-check services">
            {probes.map((p) => (
                <li key={p.label} className="flex items-center gap-1.5" title={`${p.label}: ${p.detail}`}>
                    <span
                        aria-hidden
                        className={
                            'inline-block h-1.5 w-1.5 rounded-full ' +
                            (p.up === null ? 'bg-mist' : p.up ? 'bg-obsidian' : 'bg-ember-orange')
                        }
                    />
                    <span className="sr-only">
                        {p.up === null ? 'checking' : p.up ? 'live' : `down: ${p.detail}`}
                    </span>
                </li>
            ))}
        </ul>
    )
}
