import { useCallback, useEffect, useState } from 'react'
import { listDossiers, type ArchiveRow } from '../../api/audit'

/** The outcome of a load, tagged with the inputs that produced it. */
type Loaded = {
    key: string
    rows: ArchiveRow[]
    error: boolean
}

export function useDossierArchive() {
    const [reloadToken, setReloadToken] = useState(0)
    const key = String(reloadToken)

    const [loaded, setLoaded] = useState<Loaded | null>(null)

    useEffect(() => {
        let cancelled = false

        listDossiers()
            .then((rows) => {
                if (!cancelled) setLoaded({ key, rows, error: false })
            })
            .catch(() => {
                if (!cancelled) setLoaded({ key, rows: [], error: true })
            })

        return () => {
            cancelled = true
        }
    }, [key])

    const current = loaded && loaded.key === key ? loaded : null

    const reload = useCallback(() => setReloadToken((n) => n + 1), [])

    return {
        rows: current?.rows ?? [],
        loading: current === null,
        error: current?.error ?? false,
        reload,
    }
}
