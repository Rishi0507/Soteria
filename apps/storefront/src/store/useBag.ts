import { createContext, useContext } from 'react'
import type { Product } from '../catalog'

export type Line = { product: Product; qty: number }
export type Bag = {
    lines: Line[]
    count: number
    add: (p: Product) => void
    remove: (gtin: string) => void
}

export const BagContext = createContext<Bag | null>(null)

export function useBag(): Bag {
    const b = useContext(BagContext)
    if (!b) throw new Error('useBag outside BagProvider')
    return b
}
