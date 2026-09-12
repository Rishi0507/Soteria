import type { Meta, StoryObj } from '@storybook/react-vite'
import { SafeLotBadge } from './SafeLotBadge'
import { useLotStatus } from './useLotStatus'

const meta: Meta<typeof SafeLotBadge> = {
    title: 'Storefront/SafeLotBadge',
    component: SafeLotBadge,
}
export default meta

type Story = StoryObj<typeof SafeLotBadge>

function LiveBadge({ gtin, lotCode }: { gtin: string; lotCode?: string }) {
    const { status, loading, error } = useLotStatus(gtin, lotCode)
    return <SafeLotBadge status={status} loading={loading} error={error} />
}

export const Safe: Story = {
    args: {
        status: {
            gtin: '00012345678905',
            lot_code: 'L2408A',
            verdict: 'SAFE',
            checked_at: '2026-09-12T10:00:00Z',
        },
    },
}

export const Affected: Story = {
    args: {
        status: {
            gtin: '00012345678905',
            lot_code: 'L2408B',
            verdict: 'AFFECTED',
            incident_id: 'INC-2026-0412',
            hazard: 'Undeclared peanut',
            confidence: 0.97,
            checked_at: '2026-09-12T10:00:00Z',
        },
    },
}

export const UnknownLot: Story = {
    args: {
        status: {
            gtin: '00012345678905',
            lot_code: 'L9999Z',
            verdict: 'UNKNOWN_LOT',
            checked_at: '2026-09-12T10:00:00Z',
        },
    },
}

export const Loading: Story = {
    args: { status: null, loading: true },
}

export const Unavailable: Story = {
    args: { status: null, error: true },
}

export const LiveSafe: Story = {
    render: () => <LiveBadge gtin="00012345678905" lotCode="L2408A" />,
}

export const LiveAffected: Story = {
    render: () => <LiveBadge gtin="00012345678905" lotCode="L2408B" />,
}

export const LiveUnknown: Story = {
    render: () => <LiveBadge gtin="00012345678905" lotCode="L9999Z" />,
}

export const LiveServerError: Story = {
    render: () => <LiveBadge gtin="00012345678905" lotCode="BOOM" />,
}