<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { tick } from 'svelte';

  import { formatWait } from '../language/format.ts';
  import { getTranslator } from '../language/translator.svelte.ts';
  import { MAX_REASON_BYTES, type ReportOutcome } from './report.ts';

  interface Props {
    readonly name: string;
    readonly withLink?: boolean;
    readonly compact?: boolean;
    readonly hint?: string;
    readonly send: (link: string, reason: string) => Promise<ReportOutcome>;
    readonly onaccepted?: () => void;
  }

  const { name, withLink = false, compact = false, hint, send, onaccepted }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;

  let link = $state('');
  let reason = $state('');
  let sending = $state(false);
  let outcome = $state.raw<ReportOutcome>();
  let form: HTMLFormElement | undefined;

  const problems = $derived(outcome?.outcome === 'invalid' ? outcome.problems : []);
  const linkProblem = $derived.by(() => {
    const problem = problems.find((p) => p.startsWith('link_'));
    return problem ? translate(`report.problem.${problem}`) : '';
  });
  const reasonProblem = $derived(
    problems.includes('reason_too_long')
      ? translate('report.problem.reason_too_long', { bytes: MAX_REASON_BYTES })
      : '',
  );
  const result = $derived.by(() => {
    switch (outcome?.outcome) {
      case 'accepted': {
        return translate('report.accepted');
      }
      case 'retry_later': {
        return translate('read.retry_later', { time: formatWait(outcome.retryAfter, translator.language) });
      }
      case 'failed': {
        return translate('report.failed');
      }
      default: {
        return '';
      }
    }
  });

  const compactProblem = $derived(reasonProblem || (outcome?.outcome === 'accepted' ? '' : result));

  const rememberForm: Attachment<HTMLFormElement> = (element) => {
    form = element;
    return () => {
      form = undefined;
    };
  };

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (sending) {
      return;
    }
    sending = true;
    outcome = undefined;
    try {
      outcome = await send(link, reason);
    } finally {
      sending = false;
    }
    if (outcome.outcome === 'accepted') {
      link = '';
      reason = '';
      onaccepted?.();
    }
    await tick();
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }
</script>

<form onsubmit={submit} novalidate {@attach rememberForm}>
  {#if compact}
    <label for={`${name}-reason`} class="sr-only">{translate('report.reason')}</label>
    <textarea
      id={`${name}-reason`}
      rows="3"
      placeholder={translate('report.reason')}
      class="mt-2.5 block w-full resize-none rounded-[8px] px-3 py-2 text-[13px] text-ink well placeholder:text-ink-mute aria-invalid:shadow-[inset_0_0_0_1px_oklch(0.8_0.08_350/0.7)]"
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      spellcheck="false"
      bind:value={reason}
      aria-invalid={reasonProblem !== ''}
      aria-describedby={`${name}-reason-hint ${name}-reason-problem`}></textarea>
    <div class="mt-2 stack px-0.5 text-[11px] leading-relaxed">
      <p id={`${name}-reason-hint`} class={['text-ink-mute fade-swap', compactProblem ? 'opacity-0' : 'opacity-100']}>
        {hint ?? translate('report.reason.hint', { bytes: MAX_REASON_BYTES })}
      </p>
      <p
        id={`${name}-reason-problem`}
        role="alert"
        data-problem
        class={['text-rose-300 fade-swap', compactProblem ? 'opacity-100' : 'opacity-0']}
      >
        {compactProblem}
      </p>
    </div>
    <button
      type="submit"
      disabled={sending}
      class="btn-primary mt-2.5 flex h-11 w-full hover-lift items-center justify-center rounded-[8px] text-[13px] font-medium md:h-8"
    >
      {translate('report.submit')}
    </button>
  {:else}
    {#if withLink}
      <label for={`${name}-link`}>{translate('report.link')}</label>
      <input
        id={`${name}-link`}
        type="text"
        inputmode="url"
        autocomplete="off"
        autocapitalize="off"
        autocorrect="off"
        spellcheck="false"
        bind:value={link}
        aria-invalid={linkProblem !== ''}
        aria-describedby={`${name}-link-hint ${name}-link-problem`}
      />
      <p id={`${name}-link-hint`}>{translate('report.link.hint')}</p>
      <p id={`${name}-link-problem`} data-problem>{linkProblem}</p>
    {/if}
    <label for={`${name}-reason`}>{translate('report.reason')}</label>
    <textarea
      id={`${name}-reason`}
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      spellcheck="false"
      bind:value={reason}
      aria-invalid={reasonProblem !== ''}
      aria-describedby={`${name}-reason-hint ${name}-reason-problem`}></textarea>
    <p id={`${name}-reason-hint`}>{translate('report.reason.hint', { bytes: MAX_REASON_BYTES })}</p>
    <p id={`${name}-reason-problem`} data-problem>{reasonProblem}</p>
    <button type="submit" disabled={sending}>{translate('report.submit')}</button>
  {/if}
</form>
{#if !compact}
  <p role="status" aria-live="polite">{result}</p>
{/if}
