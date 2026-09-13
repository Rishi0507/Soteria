import type { Meta, StoryObj } from '@storybook/react-vite'
import { DossierDetailPanel } from './DossierDetailPanel'
import {
    dossierComplete,
    dossierWithNote,
    verifiedBroken,
    verifiedOK,
} from '../../mocks/auditFixtures'

const meta: Meta<typeof DossierDetailPanel> = {
    title: 'OpsConsole/DossierDetailPanel',
    component: DossierDetailPanel,
    args: { onGenerate: () => {}, onReload: () => {} },
}
export default meta

type Story = StoryObj<typeof DossierDetailPanel>

/** Full dossier, verified chain, timestamp token present. */
export const DossierDetail: Story = {
    args: {
        incidentId: dossierComplete.incident_id,
        dossier: dossierComplete,
        state: 'ready',
        verification: verifiedOK,
    },
}

/** The chain verifies — the reassuring half of the integrity answer. */
export const VerifiedChain: Story = {
    args: {
        incidentId: dossierComplete.incident_id,
        dossier: dossierComplete,
        state: 'ready',
        verification: { ...verifiedOK, events: 7 },
    },
}

/**
 * A broken chain: 200, verified:false, and a body with no content_hash and no
 * hash_algorithm. `problem` names the first bad record only.
 */
export const BrokenChain: Story = {
    args: {
        incidentId: dossierWithNote.incident_id,
        dossier: dossierWithNote,
        state: 'ready',
        verification: verifiedBroken,
    },
}

/**
 * The likely timestamp outcome: the default authority is plain HTTP and fails on
 * any offline box, so the dossier carries a note instead of a token. Both
 * not-instrumented counters sit at zero and are marked as such.
 */
export const TimestampNoteInsteadOfProof: Story = {
    args: {
        incidentId: dossierWithNote.incident_id,
        dossier: dossierWithNote,
        state: 'ready',
        verification: { ...verifiedOK, incident_id: dossierWithNote.incident_id, events: 5 },
    },
}

/** Recorded, but nobody has built a document for it. Not an error. */
export const NotGeneratedYet: Story = {
    args: {
        incidentId: 'inc-eu_rasff-2026-0917',
        dossier: null,
        state: 'not-generated',
        verification: {
            incident_id: 'inc-eu_rasff-2026-0917',
            verified: true,
            events: 2,
            content_hash:
                '7788990011aabbccddeeff00112233447788990011aabbccddeeff0011223344',
            hash_algorithm: 'sha256',
        },
    },
}

/** A 404 that cannot distinguish "never existed" from "lost in a restart". */
export const NotRecorded: Story = {
    args: {
        incidentId: 'inc-gone-2026-0001',
        dossier: null,
        state: 'not-recorded',
        verification: null,
        verifyError: true,
    },
}

export const Loading: Story = {
    args: { incidentId: dossierComplete.incident_id, dossier: null, loading: true },
}

export const LoadError: Story = {
    args: { incidentId: dossierComplete.incident_id, dossier: null, state: 'error' },
}

export const Generating: Story = {
    args: {
        incidentId: 'inc-eu_rasff-2026-0917',
        dossier: null,
        state: 'not-generated',
        generating: true,
        verification: {
            incident_id: 'inc-eu_rasff-2026-0917',
            verified: true,
            events: 2,
        },
    },
}
