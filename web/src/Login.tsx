// The login form, shown when the server needs a password.

import {useState} from 'preact/hooks';
import {api} from './api';

export function Login(props: {onLogin: () => void}) {
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    async function submit(e: Event) {
        e.preventDefault();
        setBusy(true);
        setError('');
        try {
            const err = await api.login(password);
            if (err) {
                setError(err);
            } else {
                props.onLogin();
            }
        } catch (ex) {
            setError(String(ex));
        } finally {
            setBusy(false);
        }
    }

    return (
        <main class="flex min-h-screen items-center justify-center px-4">
            <form class="surface-1 flex w-full max-w-sm flex-col gap-4" onSubmit={submit}>
                <div>
                    <h1>mitosu</h1>
                    <small>Log in to see your servers.</small>
                </div>
                <label class="flex flex-col gap-1">
                    Password
                    <input type="password" autocomplete="current-password" value={password} onInput={(e) => setPassword(e.currentTarget.value)} autofocus />
                </label>
                {error && <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">{error}</p>}
                <div class="flex justify-end gap-2">
                    <button class="btn-primary" type="submit" disabled={busy || password === ''}>
                        Log in
                    </button>
                </div>
            </form>
        </main>
    );
}
