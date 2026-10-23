import { mount } from 'svelte';

import App from './App.svelte';
import './app.css';
import { pickLanguage } from './lib/i18n.ts';

document.documentElement.lang = pickLanguage(navigator.languages);

mount(App, { target: document.body });
