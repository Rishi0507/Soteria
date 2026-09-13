/**
 * Scaffold shell. This is deliberately the whole app: enough chrome to confirm
 * the toolchain runs, and no feature surface yet.
 *
 * The three screens this console is for — the below-threshold review queue, the
 * confidence-threshold tuning surface, and the dossier archive — are not built.
 */
export default function App() {
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

            <main className="mx-auto max-w-5xl px-4 py-8">
                <p className="text-sm text-neutral-600">
                    Scaffold only — no screens yet.
                </p>
            </main>
        </div>
    )
}
