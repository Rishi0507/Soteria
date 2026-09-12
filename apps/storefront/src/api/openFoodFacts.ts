import type { ProductAllergens, Coverage } from './allergens'

const OFF_BASE = 'https://world.openfoodfacts.org/api/v2'

type OffProduct = {
    product_name?: string
    brands?: string
    allergens_tags?: string[]
    traces_tags?: string[]
    ingredients_text?: string
}

type OffResponse = {
    status: number
    product?: OffProduct
}

export async function getProductAllergens(
    gtin: string
): Promise<ProductAllergens> {
    const fields = 'product_name,brands,allergens_tags,traces_tags,ingredients_text'
    const res = await fetch(`${OFF_BASE}/product/${gtin}?fields=${fields}`)

    const base = {
        gtin,
        source: 'OPEN_FOOD_FACTS' as const,
        fetched_at: new Date().toISOString(),
    }

    if (!res.ok) {
        return { ...base, coverage: 'ABSENT', allergens: [] }
    }

    const data: OffResponse = await res.json()

    if (data.status !== 1 || !data.product) {
        return { ...base, coverage: 'ABSENT', allergens: [] }
    }

    const p = data.product
    const allergens = p.allergens_tags ?? []
    const hasAllergenField = Array.isArray(p.allergens_tags)

    const coverage: Coverage = hasAllergenField ? 'COMPLETE' : 'PARTIAL'

    return {
        ...base,
        product_name: p.product_name,
        brand: p.brands,
        coverage,
        allergens,
        traces: p.traces_tags ?? [],
        ingredients_text: p.ingredients_text,
    }
}