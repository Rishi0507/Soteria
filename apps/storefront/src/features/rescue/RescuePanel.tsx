import {
    formatMoney,
    LOT_UNATTRIBUTED,
    type Money,
    type Rescue,
    type RescueOption,
} from '../../api/rescue'

type Props = {
    rescue: Rescue | null
    loading?: boolean
    error?: boolean
    confirming?: string | null
    message?: string | null
    onConfirm: (optionId: string) => void
}

/**
 * RescuePanel presents the choice and nothing else. It never pre-selects an
 * option and never confirms on its own: swapping a product across an allergen
 * line has to be the customer's own deliberate decision.
 */
export function RescuePanel({
    rescue,
    loading,
    error,
    confirming,
    message,
    onConfirm,
}: Props) {
    if (loading) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-500">
                Loading your order…
            </section>
        )
    }

    if (error || !rescue) {
        return (
            <section className="rounded-xl border border-neutral-200 bg-white p-5 text-sm text-neutral-700">
                <p className="font-medium">We can’t load this request right now.</p>
                <p className="mt-1 text-neutral-600">
                    Your order has not been changed. Please try the link in your email again,
                    or contact support.
                </p>
            </section>
        )
    }

    const line = rescue.affected_line

    // allergen_safe is optional in the contract, so the three states are
    // meaningfully different and must not collapse into a truthiness check:
    //   true      → cleared against the recall hazard; offered normally.
    //   false     → a positive finding of danger; withheld, but counted so the
    //               customer knows something existed rather than nothing.
    //   undefined → nobody checked. Shown, because silently dropping it looks
    //               identical to "no near match exists", but never as a
    //               one-click green choice.
    const allSubstitutes = rescue.options.filter((o) => o.kind === 'SUBSTITUTE')
    const substitutes = allSubstitutes.filter((o) => o.allergen_safe === true)
    const unverified = allSubstitutes.filter((o) => o.allergen_safe === undefined)
    const withheldUnsafe = allSubstitutes.length - substitutes.length - unverified.length
    const others = rescue.options.filter((o) => o.kind !== 'SUBSTITUTE')

    const closed = rescue.status !== 'PROPOSED'
    const busy = Boolean(confirming)

    return (
        <section className="rounded-xl border border-neutral-200 bg-white p-5">
            <header className="border-b border-neutral-100 pb-4">
                <p className="text-xs font-semibold uppercase tracking-wide text-red-700">
                    Recall affecting order {rescue.order_id}
                </p>
                <h2 className="mt-1 text-lg font-semibold text-neutral-900">
                    {line.product_title ?? line.sku}
                </h2>
                <p className="mt-1 text-sm text-neutral-700">
                    {line.quantity} × {formatMoney(line.unit_price)}
                    <span className="ml-2 text-neutral-600">
                        {line.lot_code === LOT_UNATTRIBUTED
                            ? 'from one of the recalled lots'
                            : `lot ${line.lot_code}`}
                    </span>
                </p>
                {rescue.hazard && (
                    <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-900">
                        <span className="font-semibold">Do not consume this item.</span>{' '}
                        {rescue.hazard}
                    </p>
                )}
            </header>

            {closed ? (
                <Outcome rescue={rescue} />
            ) : (
                <div className="pt-4">
                    <h3 className="text-sm font-semibold text-neutral-900">
                        Choose what happens to this item
                    </h3>
                    <p className="mt-1 text-sm text-neutral-600">
                        Nothing changes until you choose. We never swap an item for you.
                    </p>

                    {substitutes.length > 0 ? (
                        <ul className="mt-3 space-y-3">
                            {substitutes.map((option) => (
                                <li key={option.option_id}>
                                    <SubstituteOption
                                        option={option}
                                        original={line.unit_price}
                                        pending={confirming === option.option_id}
                                        disabled={busy}
                                        onConfirm={onConfirm}
                                    />
                                </li>
                            ))}
                        </ul>
                    ) : (
                        <p className="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
                            We could not find a replacement we are confident is safe for you,
                            so we are not offering one.
                        </p>
                    )}

                    {withheldUnsafe > 0 && (
                        <p className="mt-3 text-sm text-neutral-600">
                            {withheldUnsafe === 1
                                ? 'One other replacement was not offered because it carries an allergen linked to this recall.'
                                : `${withheldUnsafe} other replacements were not offered because they carry an allergen linked to this recall.`}
                        </p>
                    )}

                    {unverified.length > 0 && (
                        <div className="mt-4 rounded-lg border border-amber-300 bg-amber-50 p-4">
                            <p className="text-sm font-semibold text-amber-900">
                                Not checked for allergens
                            </p>
                            <p className="mt-1 text-sm text-amber-900">
                                We could not verify{' '}
                                {unverified.length === 1 ? 'this product' : 'these products'}{' '}
                                against the allergen in this recall. We are not recommending
                                {unverified.length === 1 ? ' it' : ' them'}. Choose only if you
                                are confident it is safe for you.
                            </p>
                            <ul className="mt-3 space-y-3">
                                {unverified.map((option) => (
                                    <li key={option.option_id}>
                                        <UnverifiedOption
                                            option={option}
                                            original={line.unit_price}
                                            pending={confirming === option.option_id}
                                            disabled={busy}
                                            onConfirm={onConfirm}
                                        />
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}

                    <ul className="mt-4 space-y-2 border-t border-neutral-100 pt-4">
                        {others.map((option) => (
                            <li key={option.option_id}>
                                <button
                                    type="button"
                                    disabled={busy}
                                    onClick={() => onConfirm(option.option_id)}
                                    className="w-full rounded-lg border border-neutral-300 px-4 py-2 text-left text-sm font-medium text-neutral-800 hover:bg-neutral-50 disabled:opacity-50"
                                >
                                    {option.kind === 'REFUND'
                                        ? 'Refund this item'
                                        : 'Cancel the order'}
                                    <span className="ml-2 font-normal text-neutral-600">
                                        {option.rationale}
                                    </span>
                                    {confirming === option.option_id && (
                                        <span className="ml-2 text-neutral-500">Recording…</span>
                                    )}
                                </button>
                            </li>
                        ))}
                    </ul>
                </div>
            )}

            {message && (
                <p className="mt-4 rounded-lg bg-neutral-100 px-3 py-2 text-sm text-neutral-800">
                    {message}
                </p>
            )}

            <footer className="mt-4 text-xs text-neutral-500">
                Offer expires {new Date(rescue.expires_at).toLocaleString()} · incident{' '}
                {rescue.incident_id}
            </footer>
        </section>
    )
}

/**
 * "same price" was hardcoded, so a dearer substitute displayed its own price
 * next to a claim that it cost the same. Compare against the affected line.
 */
function priceNote(option: RescueOption, original?: Money): string {
    if (!option.unit_price) return ''
    const price = formatMoney(option.unit_price)
    if (!original || original.currency !== option.unit_price.currency) return price

    const diff = option.unit_price.amount_minor - original.amount_minor
    if (diff === 0) return `${price} · same price`

    const delta = formatMoney({
        amount_minor: Math.abs(diff),
        currency: option.unit_price.currency,
    })
    return `${price} · ${diff > 0 ? `${delta} more` : `${delta} less`}`
}

/** Only ever receives substitutes with allergen_safe === true. */
function SubstituteOption({
    option,
    original,
    pending,
    disabled,
    onConfirm,
}: {
    option: RescueOption
    original?: Money
    pending: boolean
    disabled: boolean
    onConfirm: (optionId: string) => void
}) {
    return (
        <div className="rounded-lg border border-emerald-200 bg-emerald-50/60 p-4">
            <div className="flex items-start justify-between gap-4">
                <div>
                    <p className="font-medium text-neutral-900">{option.product_title}</p>
                    <p className="mt-0.5 text-sm text-neutral-700">
                        {priceNote(option, original)}
                    </p>
                    <p className="mt-1 text-sm text-neutral-700">
                        {option.allergens && option.allergens.length > 0
                            ? `Contains: ${option.allergens.join(', ')}`
                            : 'No allergens found in this product’s data'}
                    </p>
                    <p className="mt-1 text-xs text-emerald-800">
                        Checked against the recall hazard and against the allergens of your
                        original item.
                    </p>
                </div>
                <button
                    type="button"
                    disabled={disabled}
                    onClick={() => onConfirm(option.option_id)}
                    className="shrink-0 rounded-lg bg-emerald-700 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-800 disabled:opacity-50"
                >
                    {pending ? 'Confirming…' : 'Send me this instead'}
                </button>
            </div>
        </div>
    )
}

/**
 * A substitute the service returned without an allergen verdict. Deliberately
 * not styled as an endorsement, and the button does not read like the safe one.
 */
function UnverifiedOption({
    option,
    original,
    pending,
    disabled,
    onConfirm,
}: {
    option: RescueOption
    original?: Money
    pending: boolean
    disabled: boolean
    onConfirm: (optionId: string) => void
}) {
    return (
        <div className="rounded-lg border border-amber-300 bg-white p-4">
            <p className="font-medium text-neutral-900">{option.product_title}</p>
            <p className="mt-0.5 text-sm text-neutral-700">{priceNote(option, original)}</p>
            <p className="mt-1 text-sm text-amber-900">
                Allergen information for this product is unavailable. It has not been checked
                against this recall.
            </p>
            <button
                type="button"
                disabled={disabled}
                onClick={() => onConfirm(option.option_id)}
                className="mt-3 rounded-lg border border-neutral-400 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-50 disabled:opacity-50"
            >
                {pending ? 'Confirming…' : 'Send this anyway'}
            </button>
        </div>
    )
}

function Outcome({ rescue }: { rescue: Rescue }) {
    if (rescue.status === 'EXPIRED') {
        return (
            <p className="pt-4 text-sm text-neutral-700">
                This offer expired before a choice was made. Your order was not changed;
                please contact support.
            </p>
        )
    }

    const chosen = rescue.options.find((o) => o.option_id === rescue.chosen_option_id)
    const confirmed = rescue.confirmed_at
        ? new Date(rescue.confirmed_at).toLocaleString()
        : null

    return (
        <div className="pt-4 text-sm text-neutral-800">
            <p className="font-medium text-emerald-800">Thank you, that’s recorded.</p>
            <p className="mt-1">
                {chosen?.kind === 'SUBSTITUTE'
                    ? `We are sending ${chosen.product_title} instead.`
                    : chosen?.kind === 'REFUND'
                        ? 'We are refunding this item and leaving the rest of your order as it is.'
                        : 'We are cancelling this order.'}
            </p>
            {confirmed && <p className="mt-1 text-neutral-600">Confirmed {confirmed}.</p>}
        </div>
    )
}