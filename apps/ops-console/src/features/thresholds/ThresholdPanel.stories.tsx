import type { Meta, StoryObj } from '@storybook/react-vite'
import { ThresholdPanel } from './ThresholdPanel'
import type { ContainmentConfig } from '../../api/containment'

const meta: Meta<typeof ThresholdPanel> = {
    title: 'OpsConsole/ThresholdPanel',
    component: ThresholdPanel,
    args: { onSave: () => {}, onReload: () => {} },
}
export default meta

type Story = StoryObj<typeof ThresholdPanel>

/** The service defaults: a whole-SKU hold carries the higher bar. */
const base: ContainmentConfig = {
    auto_hold_threshold: 0.85,
    sku_scope_threshold: 0.95,
    updated_by: 'ops:dana',
    updated_at: '2026-09-13T09:20:00Z',
}

export const Loaded: Story = {
    args: { config: base },
}

export const Loading: Story = {
    args: { config: null, loading: true },
}

export const LoadError: Story = {
    args: { config: null, loadError: true },
}

export const Saving: Story = {
    args: { config: base, saving: true },
}

/**
 * The service refused the values. Its own wording is shown rather than a
 * paraphrase, because it knows which bound it applied.
 */
export const SaveRejected: Story = {
    args: {
        config: base,
        notice: {
            kind: 'rejected',
            text: 'auto_hold_threshold must be in (0, 1]',
        },
    },
}

/**
 * Seeded already inverted, so the warning and its acknowledgement are on screen
 * at mount. Save stays disabled until the box is ticked — the backend accepts
 * this pair, so the UI must not pretend it is impossible, only deliberate.
 */
export const InvertedThresholds: Story = {
    args: {
        config: {
            ...base,
            auto_hold_threshold: 0.95,
            sku_scope_threshold: 0.1,
            updated_by: 'ops:sam',
            updated_at: '2026-09-13T11:05:00Z',
        },
    },
}

/**
 * The equal case. Not "easier" but "no harder" — a whole-product hold costs
 * exactly what a single-lot hold costs, when it is meant to cost more. Different
 * sentence, same acknowledgement, same disabled save.
 */
export const EqualThresholds: Story = {
    args: {
        config: {
            ...base,
            auto_hold_threshold: 0.9,
            sku_scope_threshold: 0.9,
            updated_by: 'ops:sam',
            updated_at: '2026-09-13T11:40:00Z',
        },
    },
}
