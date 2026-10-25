// Entry point: follows the system's light or dark setting and renders
// the app.

import {render} from 'preact';
import {App} from './App';
import './styles.css';
import {applyTheme} from './theme';

applyTheme();

const root = document.getElementById('app');
if (root) {
    render(<App />, root);
}
