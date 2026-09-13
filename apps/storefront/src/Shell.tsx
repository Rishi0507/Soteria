import { NavLink, Link, Outlet } from 'react-router-dom'
import { BackendStatus } from './features/status/BackendStatus'
import { useBag } from './store/useBag'

/**
 * The store chrome: three-zone transparent nav (links / wordmark / utility),
 * page outlet, and a footer that stays out of the way. No sticky header.
 */
export function Shell() {
    const bag = useBag()
    const nav = ({ isActive }: { isActive: boolean }) =>
        `link ${isActive ? '' : 'link-muted'}`

    return (
        <div className="min-h-screen bg-warm-cream text-obsidian">
            <header className="mx-auto grid max-w-[1440px] grid-cols-3 items-center px-6 py-6">
                <nav className="flex items-center gap-6">
                    <NavLink to="/" end className={nav}>
                        Shop
                    </NavLink>
                    <NavLink to="/pantry" className={nav}>
                        Pantry
                    </NavLink>
                    <NavLink to="/fresh" className={nav}>
                        Fresh
                    </NavLink>
                </nav>
                <Link to="/" className="wordmark justify-self-center">
                    Sotería
                </Link>
                <div className="flex items-center justify-end gap-6">
                    <BackendStatus />
                    <NavLink to="/orders" className={nav}>
                        Orders
                    </NavLink>
                    <NavLink to="/bag" className={nav}>
                        Bag{bag.count > 0 ? ` (${bag.count})` : ''}
                    </NavLink>
                </div>
            </header>

            <Outlet />

            <footer className="mx-auto max-w-[1440px] px-6 pb-16 pt-[100px]">
                <div className="hairline mb-6" />
                <div className="flex flex-wrap items-baseline justify-between gap-4 text-body-sm text-pebble">
                    <span className="wordmark text-obsidian">Sotería</span>
                    <span>
                        Every lot on every shelf is checked against live recall feeds. Allergen data ©
                        Open Food Facts contributors, ODbL.
                    </span>
                </div>
            </footer>
        </div>
    )
}
