import { useEffect, useRef, useState } from 'react'
import { AllergenPanel } from './features/allergens/AllergenPanel'
import { useProductAllergens } from './features/allergens/useProductAllergens'
import { SafeLotBadge } from './features/badge/SafeLotBadge'
import { useLotStatus } from './features/badge/useLotStatus'
import { RecallBanner } from './features/banner/RecallBanner'
import { RescuePanel } from './features/rescue/RescuePanel'
import { useRescue } from './features/rescue/useRescue'
import { BackendStatus } from './features/status/BackendStatus'

/**
 * The product this demo shop sells: a real item under a real recall.
 *
 * Gifford's Power play fudge ice cream, openFDA H-1245-2026 (Class II),
 * withdrawn for potential foreign object contamination — rubber pieces. The
 * title is the name Open Food Facts holds for this barcode, so the page and the
 * allergen panel below it describe the same product rather than two.
 *
 * 26184 is the lot the notice actually names. CLEAN-A and CLEAN-B are the
 * uncontaminated lots seedshop stocks beside it, which is the whole point: a
 * hold takes the one lot and leaves the rest selling.
 */
const PRODUCT = {
    gtin: '860864000307',
    title: 'Power play fudge ice cream',
    variant: 'One quart (946 mL)',
    price: '$4.49',
    lots: [
        { code: '26184', hint: 'recalled lot' },
        { code: 'CLEAN-A', hint: 'clean lot' },
        { code: 'ZZ-0000', hint: 'lot we don’t carry' },
    ],
}

/**
 * Query parameters let the emailed link land straight on the customer's choice:
 *   ?rescue=<id>&token=<consent token>   the link we send
 *   ?order=ORD-1001                      lookup by order, for the account page
 *   ?lot=26184                           preselect the lot on the pack
 */
function useQuery() {
    const params = new URLSearchParams(window.location.search)
    return {
        rescueId: params.get('rescue') ?? undefined,
        orderId: params.get('order') ?? undefined,
        consentToken: params.get('token') ?? undefined,
        lot: params.get('lot') ?? undefined,
    }
}

export default function App() {
    const query = useQuery()
    const [lot, setLot] = useState(query.lot ?? PRODUCT.lots[0].code)
    const [input, setInput] = useState(lot)
    const rescueRef = useRef<HTMLDivElement>(null)

    const { status, loading, error } = useLotStatus(PRODUCT.gtin, lot)
    const allergens = useProductAllergens(PRODUCT.gtin)
    const rescue = useRescue({
        rescueId: query.rescueId,
        orderId: query.orderId ?? 'ORD-1001',
        consentToken: query.consentToken,
    })

    useEffect(() => {
        document.title = `${PRODUCT.title} — Sotería`
    }, [])

    return (
        <div className="min-h-screen bg-neutral-50 text-neutral-900">
            <RecallBanner
                status={status}
                onReview={() => rescueRef.current?.scrollIntoView({ behavior: 'smooth' })}
            />

            <header className="border-b border-neutral-200 bg-white">
                <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-4">
                    <div>
                        <p className="text-lg font-semibold tracking-tight">Sotería Market</p>
                        <p className="text-xs text-neutral-500">
                            Every lot checked against live recall feeds
                        </p>
                    </div>
                    <BackendStatus />
                </div>
            </header>

            <main className="mx-auto max-w-5xl space-y-8 px-4 py-8">
                <section className="grid gap-8 md:grid-cols-2">
                    <div className="flex items-center justify-center rounded-xl border border-neutral-200 bg-white p-10">
                        <div className="text-center">
                            <div className="mx-auto h-32 w-32 rounded-2xl bg-gradient-to-br from-amber-200 to-amber-400" />
                            <p className="mt-4 text-xs uppercase tracking-wide text-neutral-500">
                                GTIN {PRODUCT.gtin}
                            </p>
                        </div>
                    </div>

                    <div>
                        <h1 className="text-2xl font-semibold tracking-tight">{PRODUCT.title}</h1>
                        <p className="mt-1 text-neutral-600">
                            {PRODUCT.variant} · {PRODUCT.price}
                        </p>

                        <div className="mt-4">
                            <SafeLotBadge status={status} loading={loading} error={error} />
                        </div>

                        <form
                            className="mt-5"
                            onSubmit={(e) => {
                                e.preventDefault()
                                setLot(input.trim())
                            }}
                        >
                            <label
                                htmlFor="lot"
                                className="block text-sm font-medium text-neutral-800"
                            >
                                Check the lot code on your pack
                            </label>
                            <div className="mt-1 flex gap-2">
                                <input
                                    id="lot"
                                    value={input}
                                    onChange={(e) => setInput(e.target.value)}
                                    placeholder="e.g. 26184"
                                    className="w-48 rounded-lg border border-neutral-300 px-3 py-2 text-sm"
                                />
                                <button
                                    type="submit"
                                    className="rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800"
                                >
                                    Check
                                </button>
                            </div>
                            <div className="mt-2 flex flex-wrap gap-2">
                                {PRODUCT.lots.map((l) => (
                                    <button
                                        key={l.code}
                                        type="button"
                                        onClick={() => {
                                            setInput(l.code)
                                            setLot(l.code)
                                        }}
                                        className="rounded-full border border-neutral-300 px-3 py-1 text-xs text-neutral-700 hover:bg-white"
                                    >
                                        {l.code}
                                        <span className="ml-1 text-neutral-500">({l.hint})</span>
                                    </button>
                                ))}
                            </div>
                        </form>

                        <div className="mt-6">
                            <h2 className="text-sm font-semibold text-neutral-900">
                                Ingredients and allergens
                            </h2>
                            <div className="mt-2">
                                <AllergenPanel data={allergens.data} loading={allergens.loading} />
                            </div>
                        </div>
                    </div>
                </section>

                <div ref={rescueRef}>
                    <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-neutral-500">
                        Your order
                    </h2>
                    <RescuePanel
                        rescue={rescue.rescue}
                        loading={rescue.loading}
                        error={rescue.error}
                        confirming={rescue.confirming}
                        message={rescue.message}
                        onConfirm={rescue.confirm}
                    />
                    {!query.consentToken && rescue.rescue?.status === 'PROPOSED' && (
                        <p className="mt-2 text-xs text-neutral-500">
                            Confirming requires the token from your email link
                            (<code>?rescue=…&amp;token=…</code>). Without it the service refuses
                            the change, by design.
                        </p>
                    )}
                </div>
            </main>

            <footer className="mx-auto max-w-5xl px-4 pb-10 text-xs text-neutral-500">
                Lot verdicts come from the resolution service; rescue options from the order
                rescue service. Allergen data is Open Food Facts.
            </footer>
        </div>
    )
}
