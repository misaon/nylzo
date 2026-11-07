<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { Fact, ReadPage } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();

  const readyBarShown = $derived((page.phase === 'ready' || page.phase === 'opening') && !page.askingDelete);
  const afterBarShown = $derived((page.phase === 'revealed' || page.phase === 'notice') && !page.askingDelete);
  const retryable = $derived(page.shown.view === 'failed' || page.shown.view === 'retry_later');
  const restShown = $derived(page.revealed !== undefined && (page.revealed.burnable || page.restDeleted));
</script>

{#snippet facts(list: readonly Fact[], className: ClassValue)}
  <ul class={['flex flex-wrap items-center justify-start gap-x-5 gap-y-1.5 text-caption text-ink-soft', className]}>
    {#each list as fact (fact.text)}
      <li class={['flex items-center gap-1.5 whitespace-nowrap', fact.accent && 'text-nebula-300']}>
        <Icon name={fact.icon} class={['size-4 shrink-0', !fact.accent && 'text-ink-mute']} />
        {fact.text}
      </li>
    {/each}
  </ul>
{/snippet}

<div class="relative border-t border-white/8">
  <div class="stack">
    <div
      {@attach page.focus.attach('bar')}
      class={[
        'flex flex-col justify-center gap-2 p-3 fade-swap max-sm:px-5 max-sm:pt-4 max-sm:pb-5 sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:pl-4',
        readyBarShown ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!readyBarShown}
    >
      {@render facts(page.readyFacts, [
        'min-h-6 py-0.5 transition-opacity duration-500 max-sm:w-full sm:min-h-5 sm:gap-x-4 sm:px-2 sm:py-0',
        page.phase === 'opening' ? 'pointer-events-none opacity-40' : 'opacity-100',
      ])}
    </div>

    <div
      class={[
        'flex flex-col justify-center p-3 fade-swap max-sm:gap-4 max-sm:px-5 max-sm:pt-4 max-sm:pb-5 sm:flex-row sm:items-center sm:gap-3 sm:pl-4',
        page.afterFacts.length > 0 ? 'sm:justify-between' : 'sm:justify-end',
        afterBarShown ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!afterBarShown}
    >
      {#if page.afterFacts.length > 0}
        {@render facts(
          page.afterFacts,
          'max-sm:-mx-5 max-sm:border-b max-sm:border-white/8 max-sm:px-5 max-sm:pt-0.5 max-sm:pb-4 sm:gap-x-4 sm:px-2',
        )}
      {/if}
      <div class="flex items-center gap-2 max-sm:w-full">
        {#if restShown}
          <Button
            {@attach page.focus.attach('deleteRest')}
            variant="danger"
            disabled={!(page.revealed?.burnable ?? false) || page.deleting}
            onclick={page.askDelete}
            class="max-sm:flex-1"
          >
            <Icon name="trash" class="size-4" animated />
            <span class="sm:hidden" aria-hidden="true">{translate('read.delete.rest.short')}</span>
            <span class="max-sm:sr-only">{translate('read.delete.rest')}</span>
          </Button>
        {/if}
        {#if retryable}
          <Button variant="primary" onclick={page.retry} class="max-sm:flex-1">
            <Icon name="refresh" class="size-4 shrink-0" animated />
            {translate('action.retry')}
          </Button>
        {:else}
          <Button variant="primary" href="/" class="max-sm:flex-1">
            <Icon name="pen" class="size-3.5 shrink-0 opacity-70" animated />
            {translate('nav.write')}
          </Button>
        {/if}
      </div>
    </div>

    <div
      role="alertdialog"
      tabindex="-1"
      aria-labelledby="{id}-confirm-title"
      aria-describedby="{id}-confirm"
      onkeydown={page.onConfirmKeydown}
      class={[
        'flex flex-col justify-center gap-4 p-3 fade-swap max-sm:px-5 max-sm:pt-4 max-sm:pb-5 sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:pl-4',
        page.askingDelete ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!page.askingDelete}
    >
      <div class="min-w-0">
        <p id="{id}-confirm-title" class="text-ui font-medium text-ink">
          {translate(page.revealed ? 'read.delete.rest.confirm' : 'read.delete.confirm.title')}
        </p>
        <p id="{id}-confirm" class="mt-0.5 text-caption text-pretty text-ink-mute max-sm:sr-only">
          {translate('read.delete.confirm')}
        </p>
      </div>
      <div class="grid grid-cols-2 gap-1.5 sm:flex sm:items-center">
        <Button {@attach page.focus.attach('keepMessage')} variant="ghost" onclick={page.cancelDelete}>
          {translate('read.delete.no')}
        </Button>
        <Button variant="danger" disabled={!page.askingDelete} onclick={page.burn}>
          <Icon name="trash" class="size-4 shrink-0" animated />
          {translate('read.delete.yes')}
        </Button>
      </div>
    </div>
  </div>
</div>
