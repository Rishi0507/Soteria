import { useState } from 'react'
import type { ActionStatus } from './api/containment'
import { DossierArchivePanel } from './features/dossiers/DossierArchivePanel'
import { DossierDetailPanel } from './features/dossiers/DossierDetailPanel'
import { useDossier } from './features/dossiers/useDossier'
import { useDossierArchive } from './features/dossiers/useDossierArchive'
import { ActionDetailPanel } from './features/queue/ActionDetailPanel'
import { ReviewQueuePanel } from './features/queue/ReviewQueuePanel'
import { useActionReview } from './features/queue/useActionReview'
import { useReviewQueue } from './features/queue/useReviewQueue'
import { ThresholdPanel } from './features/thresholds/ThresholdPanel'
import { useThresholds } from './features/thresholds/useThresholds'

/**
 * The ops console shell: the review queue, the dossier archive and threshold
 * tuning.
 */
export default function App() {
    const [status, setStatus] = useState<ActionStatus | undefined>('PENDING_REVIEW')
    const [selectedId, setSelectedId] = useState<string | undefined>()
    const [selectedIncident, setSelectedIncident] = useState<string | undefined>()

    const queue = useReviewQueue(status)
    const review = useActionReview(selectedId)
    const archive = useDossierArchive()
    const dossier = useDossier(selectedIncident)
    const thresholds = useThresholds()

    return (
        <div className="min-h-screen bg-neutral-50 text-neutral-900">
            <header className="border-b border-neutral-200 bg-white">
                <div className="mx-auto max-w-5xl px-4 py-4">
                    <p className="text-lg font-semibold tracking-tight">Sotería Ops Console</p>
                    <p className="text-xs text-neutral-500">
                        Internal. Recall containment review and incident proof.
                    </p>
                </div>
            </header>

            <main className="mx-auto max-w-5xl space-y-8 px-4 py-8">
                <ReviewQueuePanel
                    actions={queue.actions}
                    status={status}
                    selectedId={selectedId}
                    loading={queue.loading}
                    error={queue.error}
                    onStatusChange={(next) => {
                        setStatus(next)
                        setSelectedId(undefined)
                    }}
                    onSelect={setSelectedId}
                    onReload={queue.reload}
                />

                {selectedId && (
                    <ActionDetailPanel
                        action={review.action}
                        loading={review.loading}
                        error={review.error}
                        deciding={review.deciding}
                        notice={review.notice}
                        onConfirm={(body) => {
                            void review.confirm(body).then(queue.reload)
                        }}
                        onReject={(body) => {
                            void review.reject(body).then(queue.reload)
                        }}
                        onReload={review.reload}
                    />
                )}

                <DossierArchivePanel
                    rows={archive.rows}
                    selectedId={selectedIncident}
                    loading={archive.loading}
                    error={archive.error}
                    onSelect={setSelectedIncident}
                    onReload={archive.reload}
                />

                {selectedIncident && (
                    <DossierDetailPanel
                        incidentId={selectedIncident}
                        dossier={dossier.dossier}
                        state={dossier.state}
                        loading={dossier.loading}
                        generating={dossier.generating}
                        notice={dossier.notice}
                        verification={dossier.verification}
                        verifying={dossier.verifying}
                        verifyError={dossier.verifyError}
                        onGenerate={() => {
                            void Promise.resolve(dossier.generate()).then(archive.reload)
                        }}
                        onReload={dossier.reload}
                    />
                )}

                <ThresholdPanel
                    config={thresholds.config}
                    loading={thresholds.loading}
                    loadError={thresholds.loadError}
                    saving={thresholds.saving}
                    notice={thresholds.notice}
                    onSave={thresholds.save}
                    onReload={thresholds.reload}
                />
            </main>
        </div>
    )
}
