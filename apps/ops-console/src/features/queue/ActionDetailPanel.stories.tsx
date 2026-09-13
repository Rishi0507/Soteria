import type { Meta, StoryObj } from '@storybook/react-vite'
import { ActionDetailPanel } from './ActionDetailPanel'
import {
    partialFailureAction,
    pendingMixedScope,
    pendingMultiTarget,
    pendingSKUScope,
    pendingSingle,
    rejectedAction,
} from '../../mocks/fixtures'

const meta: Meta<typeof ActionDetailPanel> = {
    title: 'OpsConsole/ActionDetailPanel',
    component: ActionDetailPanel,
    args: { onConfirm: () => {}, onReject: () => {}, onReload: () => {} },
}
export default meta

type Story = StoryObj<typeof ActionDetailPanel>

/** One product, two lots. Both selected: the default is the full hold. */
export const ActionDetail: Story = {
    args: { action: pendingSingle },
}

/**
 * Two products sharing lot L-4472. The override is one flat list with no
 * per-product addressing, so the panel says which products each code touches.
 * Unselect L-4471 and L-4472 to see the empty-intersection block appear.
 */
export const MultiTarget: Story = {
    args: { action: pendingMultiTarget },
}

/** No lot codes were recovered, so nothing here can be narrowed at all. */
export const SKUScopeCannotNarrow: Story = {
    args: { action: pendingSKUScope },
}

/** One narrowable product, one that will be held whole whatever is selected. */
export const MixedScope: Story = {
    args: { action: pendingMixedScope },
}

export const Loading: Story = {
    args: { action: null, loading: true },
}

export const LoadError: Story = {
    args: { action: null, error: true },
}

export const Confirming: Story = {
    args: { action: pendingMultiTarget, deciding: true },
}

/**
 * The case the status hides: any target held makes the action HUMAN_CONFIRMED,
 * so a failed target only shows in results[].
 */
export const ConfirmPartialFailure: Story = {
    args: {
        action: partialFailureAction,
        notice: {
            kind: 'partial',
            text: 'Confirmed, but 1 of 2 targets did not hold. Check each result below.',
        },
    },
}

/** A rejected action: `note` holds the reviewer's reason, `reason` the queueing text. */
export const Rejected: Story = {
    args: { action: rejectedAction },
}

/**
 * Another reviewer decided first. The action shown is what it actually became,
 * and there is nothing to retry — a rejection in particular is terminal.
 */
export const Conflict: Story = {
    args: {
        action: {
            ...pendingSingle,
            status: 'HUMAN_CONFIRMED' as const,
            actor: 'ops:sam',
            decided_at: '2026-09-13T10:31:00Z',
            results: [
                {
                    gtin: '00041196910537',
                    status: 'HELD' as const,
                    platform: 'shopify',
                    units_held: 65,
                    units_left_sellable: 60,
                },
            ],
        },
        notice: {
            kind: 'conflict',
            text: 'Someone else decided this first. It is now HUMAN_CONFIRMED, by ops:sam. Your decision was not applied.',
        },
    },
}
