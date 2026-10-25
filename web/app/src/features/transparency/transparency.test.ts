import { describe, expect, it } from 'vitest';

import { contactHref } from './transparency.ts';

describe('transparency', () => {
  it('links an email address or an https address', () => {
    expect(contactHref('abuse@example.org')).toBe('mailto:abuse@example.org');
    expect(contactHref('https://example.org/contact')).toBe('https://example.org/contact');
    expect(contactHref('https://example.org:8443/report?lang=cs#form')).toBe(
      'https://example.org:8443/report?lang=cs#form',
    );
  });

  it.each([
    'javascript:alert(1)',
    'http://example.test',
    'mailto:abuse@example.org',
    'https://user:pass@example.org',
    'https://',
    'https://example.org/"onmouseover',
    'Abuse <abuse@example.org>',
    'abuse@example.org?subject=x',
    'data:text/html,x',
    '//example.org',
    'a..b@example.org',
    '.a@example.org',
    'abuse@localhost',
    'https://:443/contact',
    'https://example.org/a b',
    'https://example.org/\u{202E}',
    'abuse@example.org\u{202E}',
  ])('links nothing for %j', (contact) => {
    expect(contactHref(contact)).toBeUndefined();
  });
});
