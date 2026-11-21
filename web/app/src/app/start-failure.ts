import { BUTTON_BASE, BUTTON_SIZES, BUTTON_VARIANTS } from '../shared/ui/button/button-classes.ts';
import { CAUTION_PATHS } from '../shared/ui/icon/caution.ts';
import { CALLOUT, CALLOUT_BADGE, CALLOUT_MESSAGE } from '../shared/ui/notice/callout-classes.ts';

const SVG = 'http://www.w3.org/2000/svg';

function cautionIcon(): SVGSVGElement {
  const icon = document.createElementNS(SVG, 'svg');
  icon.setAttribute('viewBox', '0 0 24 24');
  icon.setAttribute('fill', 'none');
  icon.setAttribute('stroke', 'currentColor');
  icon.setAttribute('stroke-width', '1.9');
  icon.setAttribute('stroke-linecap', 'round');
  icon.setAttribute('stroke-linejoin', 'round');
  icon.setAttribute('aria-hidden', 'true');
  icon.classList.add('size-4.5');
  for (const data of CAUTION_PATHS) {
    const path = document.createElementNS(SVG, 'path');
    path.setAttribute('d', data);
    icon.append(path);
  }
  return icon;
}

// The Svelte runtime and the catalogs may be what failed to load, so the notice is plain DOM with built-in text.
// A read page has already dropped the key from its address, so loading it again would show the message as removed.
export function showStartFailure(reading: boolean, language: string): void {
  const text = __NYLZO_START_FAILURE__[language];
  if (text === undefined || document.querySelector('#start-failure')) {
    return;
  }
  document.documentElement.dataset['fonts'] = '';
  // A page with no prerendered content gets the notice in its middle, a prerendered page keeps it out of the way.
  const blank = document.body.childElementCount === 0;
  const notice = document.createElement('div');
  notice.id = 'start-failure';
  notice.className = [
    'fixed inset-x-4 z-50 mx-auto max-w-column rounded-card bg-void-900/90',
    blank ? 'top-1/2 -translate-y-1/2' : 'bottom-[max(1rem,env(safe-area-inset-bottom))]',
  ].join(' ');
  const callout = document.createElement('div');
  callout.className = CALLOUT;
  const badge = document.createElement('span');
  badge.className = CALLOUT_BADGE;
  badge.append(cautionIcon());
  const message = document.createElement('p');
  message.setAttribute('role', 'alert');
  message.className = CALLOUT_MESSAGE;
  message.textContent = reading ? text.reopen : text.message;
  const retry = document.createElement('button');
  retry.type = 'button';
  retry.className = [BUTTON_VARIANTS.ghost, BUTTON_BASE, BUTTON_SIZES.compact].join(' ');
  retry.textContent = text.retry;
  retry.addEventListener('click', () => {
    location.reload();
  });
  callout.append(badge, message);
  if (!reading) {
    callout.append(retry);
  }
  notice.append(callout);
  document.body.append(notice);
}
