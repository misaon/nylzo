<script lang="ts">
  import type { MessageKey } from '../language/czech.ts';
  import type { LinkProblem } from './link-handoff.ts';

  import { getTranslator } from '../language/translator.svelte.ts';
  import Icon from '../ui/Icon.svelte';
  import { PRIVATE_TEXT } from '../ui/private-fields.ts';
  import { getLinkOpener } from './link-opener.ts';

  interface Props {
    readonly heading?: boolean;
  }

  const PROBLEMS: Readonly<Record<LinkProblem, MessageKey>> = {
    foreign: 'report.problem.link_foreign',
    invalid: 'report.problem.link_invalid',
    newer: 'report.problem.link_newer',
    released: 'report.problem.link_released',
    retired: 'report.problem.link_retired',
  };

  const { heading = false }: Props = $props();
  const { translate } = getTranslator();
  const openLink = getLinkOpener();
  const id = $props.id();

  let link = $state('');
  let problem = $state<LinkProblem>();

  function open(event: SubmitEvent) {
    event.preventDefault();
    const typed = link;
    link = '';
    problem = openLink(typed);
  }
</script>

<form class="w-full text-left" novalidate onsubmit={open} aria-labelledby="{id}-title">
  <p id="{id}-title" class={heading ? 'font-display text-lg font-light text-ink' : 'text-[13px] text-ink'}>
    {translate('paste.title')}
  </p>
  <p id="{id}-hint" class="mt-1 text-[13px] leading-relaxed text-pretty text-ink-mute">{translate('paste.hint')}</p>
  <div class="mt-3 flex gap-2">
    <label for="{id}-link" class="sr-only">{translate('paste.label')}</label>
    <input
      id="{id}-link"
      type="text"
      inputmode="url"
      {...PRIVATE_TEXT}
      bind:value={link}
      oninput={() => (problem = undefined)}
      placeholder={translate('paste.placeholder')}
      aria-invalid={problem !== undefined}
      aria-describedby="{id}-hint {id}-problem"
      class="h-11 min-w-0 flex-1 rounded-[9px] px-3 font-mono text-[13px] text-ink well placeholder:font-sans placeholder:text-ink-mute md:h-10"
    />
    <button
      type="submit"
      class="btn-primary flex h-11 shrink-0 hover-lift items-center gap-2 rounded-[9px] px-4 text-[13px] font-medium md:h-10"
    >
      <Icon name="unlock" class="size-4 shrink-0" />
      {translate('paste.submit')}
    </button>
  </div>
  <p id="{id}-problem" role="alert" class="mt-1.5 min-h-[1lh] text-[13px] text-rose-300">
    {problem ? translate(PROBLEMS[problem]) : ''}
  </p>
</form>
