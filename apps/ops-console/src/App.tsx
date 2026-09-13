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
 * tuning — three sections floating on the void, each a headline + copy on one
 * side and the working panel on the other.
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

    const waiting = queue.actions?.filter((a) => a.status === 'PENDING_REVIEW').length ?? 0

    return (
        <div className="min-h-screen bg-warm-cream text-obsidian">
            <div>
                <header className="mx-auto grid max-w-[1440px] grid-cols-3 items-center px-6 py-6">
                    <nav className="flex items-center gap-6 text-body-sm">
                        <a href="#queue" className="link link-muted">Queue</a>
                        <a href="#dossiers" className="link link-muted">Dossiers</a>
                        <a href="#thresholds" className="link link-muted">Thresholds</a>
                    </nav>
                    <a href="/" className="wordmark justify-self-center">Sotería</a>
                    <p className="justify-self-end text-body-sm text-pebble">Operations</p>
                </header>

                <main className="mx-auto max-w-[1440px] px-6">
                    {/* Hero / queue */}
                    <section id="queue" className="grid gap-12 pt-10 pb-[100px] md:grid-cols-[0.8fr_1.2fr]">
                        <div>
                            <p className="label">Containment review</p>
                            <h1 className="headline-lg mt-3">
                                {waiting === 0 ? 'Nothing waiting on you.' : waiting === 1 ? 'One call to make.' : `${waiting} calls to make.`}
                            </h1>
                            <p className="copy mt-6">
                                Sotería holds a lot on its own when the match is certain. When it isn’t,
                                the proposed action lands here with its evidence, and nothing moves until
                                a person decides.
                            </p>
                        </div>
                        <div className="md:pt-8">
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
                                <div className="mt-12">
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
                                </div>
                            )}
                        </div>
                    </section>

                    {/* Dossiers: panel left, copy right (zig-zag) */}
                    <section id="dossiers" className="grid gap-12 pb-[100px] md:grid-cols-[1.2fr_0.8fr]">
                        <div className="order-2 md:order-1 md:pt-8">
                            <DossierArchivePanel
                                rows={archive.rows}
                                selectedId={selectedIncident}
                                loading={archive.loading}
                                error={archive.error}
                                onSelect={setSelectedIncident}
                                onReload={archive.reload}
                            />
                            {selectedIncident && (
                                <div className="mt-12">
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
                                </div>
                            )}
                        </div>
                        <div className="order-1 md:order-2">
                            <p className="label">Proof</p>
                            <h2 className="headline mt-3">Every incident, sealed.</h2>
                            <p className="copy mt-6">
                                A dossier is the timestamped record of what was known, what was held,
                                and who was told — hashed so it can be handed to an insurer or a
                                regulator and verified without trusting us.
                            </p>
                        </div>
                    </section>

                    {/* Thresholds */}
                    <section id="thresholds" className="grid gap-12 pb-[100px] md:grid-cols-[0.8fr_1.2fr]">
                        <div>
                            <p className="label">Thresholds</p>
                            <h2 className="headline mt-3">How sure is sure enough.</h2>
                            <p className="copy mt-6">
                                Above the auto-hold threshold Sotería acts alone; below it, a person
                                reviews. Tune it here — it takes effect immediately, no redeploy.
                            </p>
                        </div>
                        <div className="md:pt-8">
                            <ThresholdPanel
                                config={thresholds.config}
                                loading={thresholds.loading}
                                loadError={thresholds.loadError}
                                saving={thresholds.saving}
                                notice={thresholds.notice}
                                onSave={thresholds.save}
                                onReload={thresholds.reload}
                            />
                        </div>
                    </section>
                </main>

                <footer className="mx-auto max-w-[1440px] px-6 pb-16 pt-8 text-caption text-pebble">
                    Internal. Recall containment review and incident proof.
                </footer>
            </div>
        </div>
    )
}

