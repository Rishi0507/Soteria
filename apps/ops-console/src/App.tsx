import { ThresholdPanel } from './features/thresholds/ThresholdPanel'
import { useThresholds } from './features/thresholds/useThresholds'

/**
 * The ops console shell. One screen so far: threshold tuning.
 *
 * The review queue and the dossier archive are not built.
 */
export default function App() {
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
