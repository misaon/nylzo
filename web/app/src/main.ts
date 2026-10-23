import { mount } from 'svelte';

import App from './App.svelte';
import './app.css';
import { browserPreferences, savedLanguage } from './lib/i18n.svelte.ts';

document.documentElement.lang = savedLanguage(browserPreferences(), navigator.languages);

mount(App, { target: document.body });
