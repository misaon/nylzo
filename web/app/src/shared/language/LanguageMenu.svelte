<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';
  import { on } from 'svelte/events';

  import Button from '../ui/button/Button.svelte';
  import Icon from '../ui/icon/Icon.svelte';
  import Popover from '../ui/Popover.svelte';
  import type { Language } from './language.ts';
  import LanguageFlag from './LanguageFlag.svelte';
  import { getTranslator } from './translator.svelte.ts';

  interface Props {
    readonly placement: 'header' | 'footer';
    readonly onlanguage: (language: Language) => void;
    readonly class?: ClassValue | undefined;
  }

  const NAMES: Readonly<Record<Language, string>> = { cs: 'Čeština', en: 'English' };
  const ORDER: readonly Language[] = ['cs', 'en'];
  const SPOKEN_NAMES = { cs: 'language.name.cs', en: 'language.name.en' } as const satisfies Record<Language, string>;

  const { placement, onlanguage, class: className }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();
  let open = $state(false);
  const current = $derived(translator.language);
  const currentLabel = $derived(translate('language.current', { language: translate(SPOKEN_NAMES[current]) }));

  function choose(language: Language) {
    open = false;
    onlanguage(language);
  }

  function onkeydown(event: KeyboardEvent, language: Language) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      choose(language);
      return;
    }
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') {
      return;
    }
    event.preventDefault();
    const option = event.currentTarget as HTMLElement;
    const options = [...(option.parentElement?.querySelectorAll<HTMLElement>('[role="option"]') ?? [])];
    const index = options.indexOf(option);
    options.at((index + (event.key === 'ArrowDown' ? 1 : -1)) % options.length)?.focus();
  }

  const dismissOutside: Attachment<HTMLElement> = (element) =>
    on(globalThis, 'pointerdown', (event) => {
      if (open && !(event.target instanceof Node && element.contains(event.target))) {
        open = false;
      }
    });
</script>

<div
  class={[placement === 'footer' ? 'relative w-fit' : 'relative flex', className]}
  data-menu
  {@attach dismissOutside}
>
  <Button
    variant="ghost"
    size={placement === 'footer' ? 'compact' : 'tight'}
    aria-haspopup="dialog"
    aria-expanded={open}
    aria-controls={id}
    aria-label={placement === 'footer' ? currentLabel : `${current.toUpperCase()}, ${currentLabel}`}
    onclick={() => (open = !open)}
  >
    {#if placement === 'footer'}
      <Icon name="globe" class="size-4 shrink-0 text-aegis-300" animated />
      <span>{NAMES[current]}</span>
      <Icon name="chevronsUpDown" class="size-3.5 shrink-0 opacity-70" stroke={2} />
    {:else}
      <Icon name="globe" class="size-4 shrink-0" />
      <span>{current.toUpperCase()}</span>
    {/if}
  </Button>
  <Popover
    {id}
    label={translate('language.label')}
    {open}
    from={placement === 'footer' ? 'below' : 'above'}
    onclose={() => (open = false)}
    class={[
      'w-[min(18rem,calc(100vw-3rem))] p-1.5',
      placement === 'footer' ? 'bottom-full left-0 mb-2 sm:right-0 sm:left-auto' : 'top-full right-0 mt-2',
    ]}
  >
    <ul role="listbox" aria-label={translate('language.list')} class="-mx-0.5 grid gap-0.5 overflow-auto px-0.5 py-0.5">
      {#each ORDER as language (language)}
        {const selected = $derived(language === current)}
        <li
          role="option"
          aria-selected={selected}
          lang={language}
          tabindex={selected ? 0 : -1}
          onclick={() => choose(language)}
          onkeydown={(event) => onkeydown(event, language)}
          class={[
            'flex h-11 cursor-pointer items-center gap-3 rounded-control px-2.5 text-ui transition md:h-10',
            selected
              ? 'bg-aegis-500/12 text-ink shadow-rim ring-1 ring-aegis-400/35'
              : 'text-ink-soft hover:bg-white/6 hover:text-ink',
          ]}
        >
          <LanguageFlag {language} />
          <span class="flex-1 text-left">{NAMES[language]}</span>
          {#if selected}
            <Icon name="check" class="size-4 shrink-0 text-aegis-200 drop-shadow-[0_0_6px_oklch(0.86_0.06_246/0.7)]" />
          {:else}
            <span class="size-4 shrink-0" aria-hidden="true"></span>
          {/if}
        </li>
      {/each}
    </ul>
  </Popover>
</div>
