import { useEffect, useState } from 'react'
import { getProductAllergens } from '../../api/openFoodFacts'
import type { ProductAllergens } from '../../api/allergens'

export function useProductAllergens(gtin?: string) {
    const [data, setData] = useState<ProductAllergens | null>(null)
    const [loading, setLoading] = useState(Boolean(gtin))

    useEffect(() => {
        if (!gtin) {
            setData(null)
            setLoading(false)
            return
        }

        let cancelled = false
        setLoading(true)

        getProductAllergens(gtin)
            .then((r) => { if (!cancelled) { setData(r); setLoading(false) } })
            .catch(() => {
                if (!cancelled) {
                    setData({
                        gtin,
                        source: 'OPEN_FOOD_FACTS',
                        fetched_at: new Date().toISOString(),
                        coverage: 'ABSENT',
                        allergens: [],
                    })
                    setLoading(false)
                }
            })

        return () => { cancelled = true }
    }, [gtin])

    return { data, loading }
}