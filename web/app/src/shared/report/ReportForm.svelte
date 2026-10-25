<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { tick } from 'svelte';

  import type { ReportStatus } from '../api/api.ts';

  import { formatWait } from '../language/format.ts';
  import { getTranslator } from '../language/translator.svelte.ts';
  import { MAX_REASON_BYTES, type ReportOutcome, type ReportSubmission } from './report.ts';

  interface Props {
    readonly name: string;
    readonly withLink?: boolean;
    readonly compact?: boolean;
    readonly hint?: string;
    readonly onsubmit: (submission: ReportSubmission) => Promise<ReportOutcome>;
    readonly onsent?: (status: ReportStatus) => void;
  }

  const { name, withLink = false, compact = false, hint, onsubmit, onsent }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;

  let link = $state('');
  let reason = $state('');
  let contact = $state('');
  let goodFaith = $state(false);
  let sending = $state(false);
  let outcome = $state.raw<ReportOutcome>();
  let form: HTMLFormElement | undefined;

  const problems = $derived(outcome?.outcome === 'invalid' ? outcome.problems : []);
  const linkProblem = $derived.by(() => {
    const problem = problems.find((problem) => problem.startsWith('link_'));
    return problem ? translate(`report.problem.${problem}`) : '';
  });
  const reasonProblem = $derived(
    problems.includes('reason_too_long')
      ? translate('report.problem.reason_too_long', { bytes: MAX_REASON_BYTES })
      : '',
  );
  const contactProblem = $derived(
    problems.includes('contact_invalid') ? translate('report.problem.contact_invalid') : '',
  );
  const goodFaithProblem = $derived(
    problems.includes('good_faith_missing') ? translate('report.problem.good_faith_missing') : '',
  );
  const sent = $derived(outcome?.outcome === 'accepted' || outcome?.outcome === 'limit');
  const result = $derived.by(() => {
    switch (outcome?.outcome) {
      case 'accepted': {
        return translate('report.accepted');
      }
      case 'limit': {
        return translate('report.limit');
      }
      case 'retry_later': {
        return translate('read.retry_later', { time: formatWait(outcome.retryAfter, translator.language) });
      }
      case 'failed': {
        return translate('report.failed');
      }
      case 'ignored':
      case 'invalid':
      case undefined: {
        return '';
      }
    }
  });

  const compactProblem = $derived(reasonProblem || (sent ? '' : result));

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
    let answer: ReportOutcome;
    try {
      answer = await onsubmit({ link, reason, contact, goodFaith });
    } finally {
      sending = false;
    }
    outcome = answer;
    if (answer.outcome === 'accepted') {
      link = '';
      reason = '';
      contact = '';
      goodFaith = false;
    }
    if (answer.outcome === 'accepted' || answer.outcome === 'limit') {
      onsent?.(answer.outcome);
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
    <label for={`${name}-contact`} class="sr-only">{translate('report.contact')}</label>
    <input
      id={`${name}-contact`}
      type="email"
      autocomplete="email"
      autocapitalize="off"
      autocorrect="off"
      spellcheck="false"
      placeholder={translate('report.contact')}
      class="mt-2.5 block h-11 w-full rounded-[8px] px-3 text-[13px] text-ink well placeholder:text-ink-mute aria-invalid:shadow-[inset_0_0_0_1px_oklch(0.8_0.08_350/0.7)] md:h-9"
      bind:value={contact}
      aria-invalid={contactProblem !== ''}
      aria-describedby={`${name}-contact-hint ${name}-contact-problem`}
    />
    <div class="mt-2 stack px-0.5 text-[11px] leading-relaxed">
      <p id={`${name}-contact-hint`} class={['text-ink-mute fade-swap', contactProblem ? 'opacity-0' : 'opacity-100']}>
        {translate('report.contact.hint')}
      </p>
      <p
        id={`${name}-contact-problem`}
        role="alert"
        data-problem
        class={['text-rose-300 fade-swap', contactProblem ? 'opacity-100' : 'opacity-0']}
      >
        {contactProblem}
      </p>
    </div>
    <label
      class="mt-2 flex min-h-11 cursor-pointer items-start gap-2.5 px-0.5 text-[11px] leading-relaxed text-ink-soft md:min-h-0"
    >
      <input
        type="checkbox"
        required
        class="mt-0.5 size-4 min-h-0 min-w-0 shrink-0 accent-aegis-400"
        bind:checked={goodFaith}
        aria-invalid={goodFaithProblem !== ''}
        aria-describedby={`${name}-good-faith-problem`}
      />
      <span>{translate('report.good_faith')}</span>
    </label>
    <p
      id={`${name}-good-faith-problem`}
      role="alert"
      data-problem
      class="min-h-[1lh] px-0.5 text-[11px] leading-relaxed text-rose-300"
    >
      {goodFaithProblem}
    </p>
    <button
      type="submit"
      disabled={sending}
      class="btn-primary mt-1.5 flex h-11 w-full hover-lift items-center justify-center rounded-[8px] text-[13px] font-medium md:h-8"
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
    <label for={`${name}-contact`}>{translate('report.contact')}</label>
    <input
      id={`${name}-contact`}
      type="email"
      autocomplete="email"
      autocapitalize="off"
      autocorrect="off"
      spellcheck="false"
      bind:value={contact}
      aria-invalid={contactProblem !== ''}
      aria-describedby={`${name}-contact-hint ${name}-contact-problem`}
    />
    <p id={`${name}-contact-hint`}>{translate('report.contact.hint')}</p>
    <p id={`${name}-contact-problem`} data-problem>{contactProblem}</p>
    <label class="flex min-h-11 cursor-pointer items-start gap-3">
      <input
        type="checkbox"
        required
        class="mt-0.5 size-4 min-h-0 min-w-0 shrink-0 p-0 accent-aegis-400 shadow-none"
        bind:checked={goodFaith}
        aria-invalid={goodFaithProblem !== ''}
        aria-describedby={`${name}-good-faith-problem`}
      />
      <span>{translate('report.good_faith')}</span>
    </label>
    <p id={`${name}-good-faith-problem`} data-problem>{goodFaithProblem}</p>
    <button type="submit" disabled={sending}>{translate('report.submit')}</button>
  {/if}
</form>
{#if !compact}
  <div class="stack">
    <p role="status" aria-live="polite">{result}</p>
    <p class="invisible" aria-hidden="true">{translate('report.limit')}</p>
  </div>
{/if}
