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
        return <p className="text-ui text-ash-gray">Loading ingredient data…</p>
    }

    if (!data || data.coverage === 'ABSENT') {
        return (
            <div className="block text-ui text-silver-mist">
                No ingredient data available for this product. Check the packaging.
            </div>
        )
    }

    if (data.coverage === 'PARTIAL') {
        return (
            <div className="rounded-3xl border border-transparent p-3 text-ui text-saffron-spark">
                <p className="font-normal">Allergen information incomplete</p>
                <p>
                    We don't have allergen data for this product. Check the packaging
                    before consuming.
                </p>
                {data.ingredients_text && (
                    <p className="mt-2 text-saffron-spark">{data.ingredients_text}</p>
                )}
            </div>
        )
    }

    return (
        <div className="block text-ui">
            {data.allergens.length > 0 ? (
                <>
                    <p className="font-normal text-bone-white">Contains</p>
                    <ul className="mt-1 flex flex-wrap gap-2">
                        {data.allergens.map((tag) => (
                            <li
                                key={tag}
                                className="rounded-full px-2 py-0.5 capitalize text-saffron-spark"
                            >
                                {label(tag)}
                            </li>
                        ))}
                    </ul>
                </>
            ) : (
                <p className="text-silver-mist">No allergens declared.</p>
            )}

            {data.traces && data.traces.length > 0 && (
                <p className="mt-2 text-ash-gray">
                    May contain: {data.traces.map(label).join(', ')}
                </p>
            )}

            {data.ingredients_text && (
                <p className="mt-2 text-ash-gray">{data.ingredients_text}</p>
            )}
        </div>
    )
}