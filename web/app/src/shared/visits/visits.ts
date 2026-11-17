import type { Screen } from '../../app/pages.ts';

type VisitPage = 'agents' | 'home' | 'other' | 'sent' | 'terms' | 'transparency';

type VisitSignal = 'automation' | 'person';

// A screen that shows a message is never counted (spec section 6.7).
const PAGES: Readonly<Record<Exclude<Screen, 'read'>, VisitPage>> = {
  create: 'home',
  sent: 'sent',
  transparency: 'transparency',
  terms: 'terms',
  agents: 'agents',
  notFound: 'other',
};

export const PERSON_EVENTS = ['pointerdown', 'pointermove', 'keydown', 'scroll', 'touchstart', 'wheel'] as const;

const VISITS_PATH = '/api/v1/visits';
const TIMEOUT_MS = 10_000;

type Noticed = (event: Pick<Event, 'isTrusted'>) => void;

interface Events {
  readonly addEventListener: (type: string, listener: Noticed, options: Readonly<AddEventListenerOptions>) => void;
  readonly removeEventListener: (type: string, listener: Noticed, options: Readonly<EventListenerOptions>) => void;
}

export interface VisitEnvironment {
  readonly automated: boolean;
  readonly events: Events;
  readonly send: (body: string) => void;
}

function visitPageOf(screen: Screen): VisitPage | undefined {
  return screen === 'read' ? undefined : PAGES[screen];
}

function signalBody(signal: VisitSignal, page: VisitPage): string {
  return JSON.stringify({ signal, page });
}

// Sends one signal per document: that a program drives the browser, or the first sign that a person is there.
export function watchVisit(screen: Screen, environment: VisitEnvironment): void {
  const page = visitPageOf(screen);
  if (page === undefined) {
    return;
  }
  if (environment.automated) {
    environment.send(signalBody('automation', page));
    return;
  }
  const person = signalBody('person', page);
  const options = { capture: true, passive: true };
  const stop = () => {
    for (const type of PERSON_EVENTS) {
      environment.events.removeEventListener(type, noticed, options);
    }
  };
  function noticed(event: Pick<Event, 'isTrusted'>) {
    if (!event.isTrusted) {
      return;
    }
    stop();
    environment.send(person);
  }
  for (const type of PERSON_EVENTS) {
    environment.events.addEventListener(type, noticed, options);
  }
}

async function sendVisit(body: string): Promise<void> {
  try {
    const response = await fetch(VISITS_PATH, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
      body,
      keepalive: true,
      priority: 'low',
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    await response.body?.cancel();
  } catch {}
}

export function watchBrowserVisit(screen: Screen): void {
  watchVisit(screen, { automated: navigator.webdriver, events: globalThis, send: (body) => void sendVisit(body) });
}
