<script lang="ts">
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';

  import { formatWait } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { MAX_REASON_BYTES, type ReportOutcome } from '../lib/report.ts';

  interface Props {
    readonly name: string;
    readonly withLink?: boolean;
    readonly send: (link: string, reason: string) => Promise<ReportOutcome>;
  }

  const { name, withLink = false, send }: Props = $props();
  const i18n = getI18n();
  const { t } = i18n;

  let link = $state('');
  let reason = $state('');
  let sending = $state(false);
  let outcome = $state.raw<ReportOutcome>();
  let form: HTMLFormElement | undefined;

  const problems = $derived(outcome?.outcome === 'invalid' ? outcome.problems : []);
  const linkProblem = $derived.by(() => {
    const problem = problems.find((p) => p !== 'reason_too_long');
    return problem ? t(`report.problem.${problem}`) : '';
  });
  const reasonProblem = $derived(problems.includes('reason_too_long') ? t('report.problem.reason_too_long', { bytes: MAX_REASON_BYTES }) : '');
  const result = $derived.by(() => {
    switch (outcome?.outcome) {
      case 'accepted':
        return t('report.accepted');
      case 'retry_later':
        return t('read.retry_later', { time: formatWait(outcome.retryAfter, i18n.language) });
      case 'failed':
        return t('report.failed');
      default:
        return '';
    }
  });

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
    }
    await tick();
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }
</script>

<form onsubmit={submit} novalidate {@attach rememberForm}>
  {#if withLink}
    <label for={`${name}-link`}>{t('report.link')}</label>
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
    <p id={`${name}-link-hint`}>{t('report.link.hint')}</p>
    <p id={`${name}-link-problem`} data-problem>{linkProblem}</p>
  {/if}
  <label for={`${name}-reason`}>{t('report.reason')}</label>
  <textarea
    id={`${name}-reason`}
    autocomplete="off"
    autocapitalize="off"
    autocorrect="off"
    spellcheck="false"
    bind:value={reason}
    aria-invalid={reasonProblem !== ''}
    aria-describedby={`${name}-reason-hint ${name}-reason-problem`}
  ></textarea>
  <p id={`${name}-reason-hint`}>{t('report.reason.hint', { bytes: MAX_REASON_BYTES })}</p>
  <p id={`${name}-reason-problem`} data-problem>{reasonProblem}</p>
  <button type="submit" disabled={sending}>{t('report.submit')}</button>
</form>
<p role="status" aria-live="polite">{result}</p>
