import type { Meta, StoryObj } from '@storybook/react-vite'
import { RescuePanel } from './RescuePanel'
import { useRescue } from './useRescue'
import type { Rescue } from '../../api/rescue'

const meta: Meta<typeof RescuePanel> = {
    title: 'Storefront/RescuePanel',
    component: RescuePanel,
    args: { onConfirm: () => {} },
}
export default meta

type Story = StoryObj<typeof RescuePanel>

const base: Rescue = {
    rescue_id: 'r-1',
    incident_id: 'inc-fda_enforcement-f-2291-2026',
    order_id: 'ORD-1001',
    status: 'PROPOSED',
    hazard: 'Undeclared peanut',
    proposed_at: '2026-09-12T10:00:00Z',
    expires_at: '2026-09-14T10:00:00Z',
    affected_line: {
        line_item_id: 'li-1',
        gtin: '00041196910537',
        sku: 'SF-GB-12',
        product_title: 'Sunfield Farms Chewy Granola Bars 12 ct',
        lot_code: '8H-1132',
        quantity: 2,
        unit_price: { amount_minor: 449, currency: 'USD' },
    },
    options: [
        {
            option_id: 'sub-00012345678905',
            kind: 'SUBSTITUTE',
            gtin: '00012345678905',
            product_title: 'Harvest Lane Oat and Honey Granola Bars 12 ct',
            unit_price: { amount_minor: 449, currency: 'USD' },
            allergen_safe: true,
            allergens: ['gluten'],
            rationale: 'same price, complete allergen data, no recall hazard allergen',
        },
        { option_id: 'refund', kind: 'REFUND', rationale: 'Refund this item and keep the rest of the order.' },
        { option_id: 'cancel', kind: 'CANCEL', rationale: 'Cancel the whole order.' },
    ],
}

export const Proposed: Story = { args: { rescue: base } }

export const Loading: Story = { args: { rescue: null, loading: true } }

export const Unavailable: Story = { args: { rescue: null, error: true } }

/** Confirming one option must not let the customer press another. */
export const Confirming: Story = {
    args: { rescue: base, confirming: 'sub-00012345678905' },
}

/** No substitute cleared the allergen and price checks, so none is offered. */
export const NoSafeSubstitute: Story = {
    args: { rescue: { ...base, options: base.options.filter((o) => o.kind !== 'SUBSTITUTE') } },
}

/** The lot could not be pinned to this line, so the copy must not name one. */
export const UnattributedLot: Story = {
    args: { rescue: { ...base, affected_line: { ...base.affected_line, lot_code: 'UNATTRIBUTED' } } },
}

export const ConfirmedSubstitute: Story = {
    args: {
        rescue: {
            ...base,
            status: 'CONFIRMED',
            chosen_option_id: 'sub-00012345678905',
            confirmed_at: '2026-09-12T10:05:00Z',
        },
    },
}

export const Expired: Story = { args: { rescue: { ...base, status: 'EXPIRED' } } }

export const ConsentRefused: Story = {
    args: { rescue: base, message: 'This confirmation link is not valid.' },
}

function LiveRescue({ orderId }: { orderId: string }) {
    const r = useRescue({ orderId })
    return (
        <RescuePanel
            rescue={r.rescue}
            loading={r.loading}
            error={r.error}
            confirming={r.confirming}
            message={r.message}
            onConfirm={r.confirm}
        />
    )
}

/** Hits the running order-rescue-service on :8083. */
export const LiveOrder: Story = { render: () => <LiveRescue orderId="ORD-1001" /> }
