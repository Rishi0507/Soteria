import type { Meta, StoryObj } from '@storybook/react-vite'
import { DossierArchivePanel } from './DossierArchivePanel'
import { archiveRows } from '../../mocks/auditFixtures'

const meta: Meta<typeof DossierArchivePanel> = {
    title: 'OpsConsole/DossierArchivePanel',
    component: DossierArchivePanel,
    args: { onSelect: () => {}, onReload: () => {} },
}
export default meta

type Story = StoryObj<typeof DossierArchivePanel>

/**
 * Three recorded incidents, one of which has no dossier built. The list is
 * ledger-derived, so that row is normal, not an error.
 */
export const Populated: Story = {
    args: { rows: archiveRows, selectedId: 'inc-fda_enforcement-f-2291-2026' },
}

/** The has_dossier:false row on its own — the one getDossier 404s on. */
export const NoDossierYet: Story = {
    args: { rows: [archiveRows[2]], selectedId: archiveRows[2].incident_id },
}

/**
 * The state that must not read as a clean slate: an emptied ledger and a quiet
 * period produce byte-identical responses.
 */
export const Empty: Story = {
    args: { rows: [] },
}

export const Loading: Story = {
    args: { rows: [], loading: true },
}

export const LoadError: Story = {
    args: { rows: [], error: true },
}
