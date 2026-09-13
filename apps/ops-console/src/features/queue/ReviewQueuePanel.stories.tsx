import type { Meta, StoryObj } from '@storybook/react-vite'
import { ReviewQueuePanel } from './ReviewQueuePanel'
import { queue } from '../../mocks/fixtures'

const meta: Meta<typeof ReviewQueuePanel> = {
    title: 'OpsConsole/ReviewQueuePanel',
    component: ReviewQueuePanel,
    args: {
        status: 'PENDING_REVIEW',
        onStatusChange: () => {},
        onSelect: () => {},
        onReload: () => {},
    },
}
export default meta

type Story = StoryObj<typeof ReviewQueuePanel>

/**
 * Four pending actions and one already rejected: single-target, multi-target,
 * SKU-scope and mixed-scope, which are the four shapes the detail view has to
 * handle differently.
 */
export const Populated: Story = {
    args: { actions: queue, selectedId: 'act-1002' },
}

export const Empty: Story = {
    args: { actions: [] },
}

/** An empty result under a filter reads differently from an empty queue. */
export const EmptyUnderFilter: Story = {
    args: { actions: [], status: 'FAILED' },
}

export const Loading: Story = {
    args: { actions: [], loading: true },
}

export const LoadError: Story = {
    args: { actions: [], error: true },
}
