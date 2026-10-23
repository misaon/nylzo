interface Rule {
  readonly pattern: RegExp;
  readonly reason: string;
  readonly svelteOnly?: boolean;
}

const RULES: readonly Rule[] = [
  { pattern: /\binnerHTML\b/, reason: 'innerHTML parses markup' },
  { pattern: /\bouterHTML\b/, reason: 'outerHTML parses markup' },
  { pattern: /\binsertAdjacentHTML\b/, reason: 'insertAdjacentHTML parses markup' },
  { pattern: /\bdocument\s*\.\s*write(?:ln)?\b/, reason: 'document.write parses markup' },
  { pattern: /\beval\s*\(/, reason: 'eval runs a string as code' },
  { pattern: /\bnew\s+Function\b/, reason: 'new Function runs a string as code' },
  { pattern: /\bset(?:Timeout|Interval)\s*\(\s*['"`]/, reason: 'a string timer runs a string as code' },
  { pattern: /\{@html\b/, reason: '{@html} parses markup', svelteOnly: true },
  { pattern: /\bstyle:[\w-]/, reason: 'style: directives are outside the reviewed CSS', svelteOnly: true },
  { pattern: /\sstyle\s*=/, reason: 'style attributes are blocked by the CSP', svelteOnly: true },
];

export function forbidden(path: string, source: string): string[] {
  const svelte = path.endsWith('.svelte');
  const found: string[] = [];
  source.split('\n').forEach((line, index) => {
    for (const rule of RULES) {
      if ((svelte || !rule.svelteOnly) && rule.pattern.test(line)) {
        found.push(`${path}:${index + 1}: ${rule.reason}`);
      }
    }
  });
  return found;
}

export function autofixerIsClean(output: string): boolean {
  return /\bissues: \[\]/.test(output) && /\bsuggestions: \[\]/.test(output);
}
