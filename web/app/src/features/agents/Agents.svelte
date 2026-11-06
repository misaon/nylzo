<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { TEXT_LINK } from '../../shared/ui/link/link-classes.ts';
  import { AGENT_RULES, AGENT_STEPS, DISCUSSIONS } from './agents-content.ts';
  import type { AgentLink } from './agents-content.ts';

  const { translate } = getTranslator();
</script>

{#snippet link({ label, href }: AgentLink, placement: string)}
  <a {href} class={[placement, 'text-caption', TEXT_LINK]}>{translate(label)}</a>
{/snippet}

<div class="mx-auto max-w-[1040px] animate-rise px-4 pt-16 pb-24 [animation-delay:80ms] sm:px-6 md:pt-24">
  <div class="max-w-[680px]">
    <p class="text-eyebrow text-aegis-300 uppercase">{translate('agents.eyebrow')}</p>
    <h1 class="mt-3 font-display text-page font-light text-balance text-ink md:text-page-lg">
      {translate('agents.title')}
    </h1>
    <p class="mt-4 text-lead text-pretty text-ink-soft md:text-lead-lg">{translate('agents.lead')}</p>
  </div>
  <ol class="mt-8 grid gap-3 md:grid-cols-3">
    {#each AGENT_STEPS as step, index (step.title)}
      <li class="glass-soft flex flex-col rounded-card p-5">
        <span class="grid size-8 place-items-center rounded-full text-caption font-medium text-white step-current">
          {index + 1}
        </span>
        <p class="mt-4 text-title-sm font-medium text-ink">{translate(step.title)}</p>
        <p class="mt-1.5 flex-1 text-body-sm text-pretty text-ink-soft">{translate(step.text)}</p>
        {#if step.link}
          {@render link(step.link, 'mt-3 self-start')}
        {/if}
      </li>
    {/each}
  </ol>
  <div class="mt-3 grid gap-3 md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
    <section class="glass-soft rounded-card p-6">
      <h2 class="text-title font-medium text-ink">{translate('agents.rules.title')}</h2>
      <ul class="mt-4 grid gap-2.5 text-body-sm text-pretty text-ink-soft">
        {#each AGENT_RULES as rule (rule)}
          <li class="flex gap-2.5">
            <Icon name="check" class="mt-0.5 size-4 shrink-0 text-aegis-300" stroke={2} />
            <span>{translate(rule)}</span>
          </li>
        {/each}
      </ul>
    </section>
    <section class="glass-soft rounded-card p-6">
      <h2 class="text-title font-medium text-ink">{translate('agents.missing.title')}</h2>
      <p class="mt-3 text-body-sm text-pretty text-ink-soft">{translate('agents.missing.text')}</p>
      {@render link(DISCUSSIONS, 'mt-3 inline-block')}
    </section>
  </div>
  <p class="mt-6 text-caption text-ink-mute">{translate('agents.markdown')}</p>
</div>
