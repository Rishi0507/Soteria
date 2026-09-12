import type { ProductAllergens } from '../../api/allergens'

type Props = {
    data: ProductAllergens | null
    loading?: boolean
}

function label(tag: string) {
    return tag.replace(/^en:/, '').replace(/-/g, ' ')
}

export function AllergenPanel({ data, loading }: Props) {
    if (loading) {
        return <p className="text-sm text-neutral-500">Loading ingredient data…</p>
    }

    if (!data || data.coverage === 'ABSENT') {
        return (
            <div className="rounded-lg border border-neutral-200 bg-neutral-50 p-3 text-sm text-neutral-700">
                No ingredient data available for this product. Check the packaging.
            </div>
        )
    }

    if (data.coverage === 'PARTIAL') {
        return (
            <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                <p className="font-medium">Allergen information incomplete</p>
                <p>
                    We don't have allergen data for this product. Check the packaging
                    before consuming.
                </p>
                {data.ingredients_text && (
                    <p className="mt-2 text-amber-800">{data.ingredients_text}</p>
                )}
            </div>
        )
    }

    return (
        <div className="rounded-lg border border-neutral-200 p-3 text-sm">
            {data.allergens.length > 0 ? (
                <>
                    <p className="font-medium text-neutral-900">Contains</p>
                    <ul className="mt-1 flex flex-wrap gap-2">
                        {data.allergens.map((tag) => (
                            <li
                                key={tag}
                                className="rounded-full bg-red-50 px-2 py-0.5 capitalize text-red-900"
                            >
                                {label(tag)}
                            </li>
                        ))}
                    </ul>
                </>
            ) : (
                <p className="text-neutral-700">No allergens declared.</p>
            )}

            {data.traces && data.traces.length > 0 && (
                <p className="mt-2 text-neutral-600">
                    May contain: {data.traces.map(label).join(', ')}
                </p>
            )}

            {data.ingredients_text && (
                <p className="mt-2 text-neutral-600">{data.ingredients_text}</p>
            )}
        </div>
    )
}