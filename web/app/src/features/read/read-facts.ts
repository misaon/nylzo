import { formatClock } from '../../shared/language/format.ts';
import type { Translator } from '../../shared/language/translator.svelte.ts';
import type { Fact } from './read-page.svelte.ts';

export function openedFacts(translator: Translator, opensRemaining: number, secondsLeft?: number): Fact[] {
  const { translate } = translator;
  const facts: Fact[] = [
    { icon: 'check', text: translate('open.chip.now') },
    {
      icon: 'eye',
      text: opensRemaining > 0 ? translator.plural('open.chip.opens', opensRemaining) : translate('open.chip.last'),
    },
  ];
  if (secondsLeft !== undefined) {
    facts.unshift({ icon: 'timer', text: translate('open.chip.clears', { time: formatClock(secondsLeft) }) });
  }
  return facts;
}
