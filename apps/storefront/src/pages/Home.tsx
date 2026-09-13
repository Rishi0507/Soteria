import { Link } from 'react-router-dom'
import { CATALOG, type Product } from '../catalog'

/**
 * Home: a display-type opener split to opposite edges, then an asymmetric
 * photo mosaic of what's on the shelves. Photography does the talking.
 */
export function Home({ filter }: { filter?: (p: Product) => boolean }) {
    const products = filter ? CATALOG.filter(filter) : CATALOG
    const [lead, ...rest] = products

    return (
        <main className="mx-auto max-w-[1440px] px-6">
            <section className="flex min-h-[52vh] flex-col justify-end pb-16 pt-10 md:flex-row md:items-end md:justify-between">
                <h1 className="display max-w-[9ch]">Good food, checked.</h1>
                <p className="display mt-6 max-w-[9ch] text-right md:mt-0">Every lot, every day.</p>
            </section>

            <div className="hairline" />

            {/* Mosaic: off-grid sizes and vertical offsets, not equal columns. */}
            <section className="grid grid-cols-6 gap-4 pt-10 md:gap-6">
                {lead && (
                    <Tile product={lead} className="col-span-6 md:col-span-4 md:row-span-2" tall />
                )}
                {rest.slice(0, 2).map((p, i) => (
                    <Tile key={p.gtin} product={p} className={`col-span-3 md:col-span-2 ${i === 1 ? 'md:mt-10' : ''}`} />
                ))}
                {rest.slice(2, 5).map((p, i) => (
                    <Tile key={p.gtin} product={p} className={`col-span-3 md:col-span-2 ${i === 0 ? 'md:-mt-10' : i === 2 ? 'md:mt-16' : ''}`} />
                ))}
                {rest.slice(5, 6).map((p) => (
                    <Tile key={p.gtin} product={p} className="col-span-6 md:col-span-3 md:row-span-2" tall />
                ))}
                {rest.slice(6).map((p, i) => (
                    <Tile key={p.gtin} product={p} className={`col-span-3 md:col-span-3 lg:col-span-3 ${i % 2 ? 'md:mt-10' : ''}`} />
                ))}
            </section>

            <section className="grid gap-10 pt-[100px] md:grid-cols-2 md:items-end">
                <h2 className="headline-lg">Nothing on our shelves that shouldn’t be.</h2>
                <p className="copy-lg">
                    Every product carries its lot. When a recall is announced anywhere — FDA, USDA, the
                    EU — the affected lot is pulled within seconds, the rest keep selling, and anyone who
                    already bought it hears from us first.
                </p>
            </section>
        </main>
    )
}

function Tile({ product, className = '', tall }: { product: Product; className?: string; tall?: boolean }) {
    return (
        <Link to={`/product/${product.gtin}`} className={`group block ${className}`}>
            <div className={`photo ${tall ? 'aspect-[4/5]' : 'aspect-square'} p-6 md:p-10`}>
                <img
                    src={product.image}
                    alt={product.name}
                    loading="lazy"
                    className="h-full w-full transition-transform duration-500 group-hover:scale-[1.03]"
                />
            </div>
            <div className="mt-3 flex items-baseline justify-between gap-4">
                <p className="text-body">
                    <span className="text-pebble">{product.brand} </span>
                    {product.name}
                </p>
                <p className="text-body-sm text-pebble">{product.price}</p>
            </div>
        </Link>
    )
}
