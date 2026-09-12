export type Coverage = 'COMPLETE' | 'PARTIAL' | 'ABSENT'

export type ProductAllergens = {
    gtin: string
    product_name?: string
    brand?: string
    source: 'OPEN_FOOD_FACTS'
    fetched_at: string
    coverage: Coverage
    allergens: string[]
    traces?: string[]
    ingredients_text?: string
}