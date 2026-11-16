import { describe, expect, it } from 'vitest';

import type { Screen } from '../../app/pages.ts';
import { PERSON_EVENTS, watchVisit } from './visits.ts';

type Listener = (event: Pick<Event, 'isTrusted'>) => void;

class FakeEvents {
  readonly #listeners = new Map<string, Set<Listener>>();

  addEventListener(type: string, listener: Listener): void {
    this.#listeners.set(type, (this.#listeners.get(type) ?? new Set()).add(listener));
  }

  removeEventListener(type: string, listener: Listener): void {
    this.#listeners.get(type)?.delete(listener);
  }

  dispatch(type: string, isTrusted = true): void {
    for (const listener of this.#listeners.get(type) ?? []) {
      listener({ isTrusted });
    }
  }
}

function browser(automated = false) {
  const sent: unknown[] = [];
  const events = new FakeEvents();
  return { sent, events, environment: { automated, events, send: (body: string) => sent.push(JSON.parse(body)) } };
}

describe('watchVisit', () => {
  it.each([
    ['create', 'home'],
    ['sent', 'sent'],
    ['transparency', 'transparency'],
    ['terms', 'terms'],
    ['agents', 'agents'],
    ['notFound', 'other'],
  ] as const)('reports the %s screen as the %s page once, at the first sign of a person', (screen, page) => {
    const { sent, events, environment } = browser();
    watchVisit(screen, environment);
    expect(sent).toEqual([]);
    events.dispatch('pointermove');
    events.dispatch('keydown');
    events.dispatch('scroll');
    expect(sent).toEqual([{ signal: 'person', page }]);
  });

  it.each(PERSON_EVENTS)('takes a trusted %s as the sign of a person and ignores a scripted one', (type) => {
    const { sent, events, environment } = browser();
    watchVisit('create', environment);
    events.dispatch(type, false);
    expect(sent).toEqual([]);
    events.dispatch(type);
    expect(sent).toEqual([{ signal: 'person', page: 'home' }]);
  });

  it('reports a browser that a program drives at once and only once', () => {
    const { sent, events, environment } = browser(true);
    watchVisit('terms', environment);
    events.dispatch('pointerdown');
    expect(sent).toEqual([{ signal: 'automation', page: 'terms' }]);
  });

  it.each([false, true])('never reports the message screen (automated: %s)', (automated) => {
    const { sent, events, environment } = browser(automated);
    watchVisit('read' satisfies Screen, environment);
    for (const type of PERSON_EVENTS) {
      events.dispatch(type);
    }
    expect(sent).toEqual([]);
  });
});
