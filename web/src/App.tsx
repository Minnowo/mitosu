// The page shell: asks whether a login is needed, then shows the login
// form or the page for the current route.

import {useEffect, useState} from 'preact/hooks';
import {api} from './api';
import {HostPage} from './HostPage';
import {Login} from './Login';
import {Overview} from './Overview';
import {useRoute} from './route';

type SessionState = 'checking' | 'login' | 'in' | 'error';

export function App() {
    const route = useRoute();
    const [session, setSession] = useState<SessionState>('checking');
    const [loginNeeded, setLoginNeeded] = useState(false);
    const [liveViews, setLiveViews] = useState(false);
    const [error, setError] = useState('');
    const [logoutError, setLogoutError] = useState('');

    useEffect(() => {
        api.session().then(
            (s) => {
                setLoginNeeded(s.login);
                setLiveViews(s.live_views);
                setSession(s.logged_in ? 'in' : 'login');
            },
            (e: unknown) => {
                setError(String(e));
                setSession('error');
            },
        );
    }, []);

    const loggedOut = () => setSession('login');

    if (session === 'checking') {
        return <small class="px-4">Loading...</small>;
    }
    if (session === 'error') {
        return <p class="m-4 rounded-md p-3 bg-c-error-container text-c-on-error-container">Cannot reach mitosu: {error}</p>;
    }
    if (session === 'login') {
        return <Login onLogin={() => setSession('in')} />;
    }
    return (
        <div class="flex flex-col gap-4">
            <header class="flex items-center justify-between gap-2 px-4 pt-2">
                <a href="#/">
                    <strong>mitosu</strong>
                </a>
                {loginNeeded && (
                    <div class="flex items-center gap-2">
                        {logoutError && <small>{logoutError}</small>}
                        <button
                            onClick={() =>
                                api.logout().then(loggedOut, (e: unknown) => setLogoutError(`Logging out failed: ${String(e)}`))
                            }
                        >
                            Log out
                        </button>
                    </div>
                )}
            </header>
            {route.page === 'host' ? (
                <HostPage
                    host={route.host}
                    range={route.params.get('range') ?? '24h'}
                    view={route.params.get('view') ?? 'charts'}
                    container={route.params.get('container') ?? ''}
                    liveViews={liveViews}
                    onLoggedOut={loggedOut}
                />
            ) : (
                <Overview onLoggedOut={loggedOut} />
            )}
        </div>
    );
}
