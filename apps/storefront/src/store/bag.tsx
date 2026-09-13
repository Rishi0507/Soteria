import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import type { Product } from '../catalog'

type Line = { product: Product; qty: number }
type Bag = {
    lines: Line[]
    count: number
    add: (p: Product) => void
    remove: (gtin: string) => void
}

const BagContext = createContext<Bag | null>(null)

/** A tiny in-memory bag so the shop feels like a shop. Not persisted on purpose. */
export function BagProvider({ children }: { children: ReactNode }) {
    const [lines, setLines] = useState<Line[]>([])
    const value = useMemo<Bag>(
        () => ({
            lines,
            count: lines.reduce((n, l) => n + l.qty, 0),
            add: (p) =>
                setLines((ls) => {
                    const i = ls.findIndex((l) => l.product.gtin === p.gtin)
                    if (i < 0) return [...ls, { product: p, qty: 1 }]
                    const next = ls.slice()
                    next[i] = { ...next[i], qty: next[i].qty + 1 }
                    return next
                }),
            remove: (gtin) => setLines((ls) => ls.filter((l) => l.product.gtin !== gtin)),
        }),
        [lines]
    )
    return <BagContext.Provider value={value}>{children}</BagContext.Provider>
}

export function useBag(): Bag {
    const b = useContext(BagContext)
    if (!b) throw new Error('useBag outside BagProvider')
    return b
}
