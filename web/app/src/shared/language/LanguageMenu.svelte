<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';
  import { on } from 'svelte/events';

  import Button from '../ui/button/Button.svelte';
  import Icon from '../ui/icon/Icon.svelte';
  import Popover from '../ui/menu/Popover.svelte';
  import { LANGUAGE_DETAILS, LANGUAGES } from './language.ts';
  import type { Language } from './language.ts';
  import LanguageFlag from './LanguageFlag.svelte';
  import { getTranslator } from './translator.svelte.ts';

  interface Props {
    readonly placement: 'header' | 'footer';
    readonly onlanguage: (language: Language) => void;
    readonly class?: ClassValue | undefined;
  }

  const nameOrder = new Intl.Collator('en');
  const ORDER = LANGUAGES.toSorted((first, second) =>
    nameOrder.compare(LANGUAGE_DETAILS[first].name, LANGUAGE_DETAILS[second].name),
  );

  const { placement, onlanguage, class: className }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();
  let open = $state(false);
  const current = $derived(translator.language);
  const currentName = $derived(LANGUAGE_DETAILS[current].name);
  const currentCode = $derived(current.split('-', 1)[0]?.toUpperCase());
  const currentLabel = $derived(translate('language.current', { language: currentName }));

  function choose(language: Language) {
    open = false;
    onlanguage(language);
  }

  function onkeydown(event: KeyboardEvent & { readonly currentTarget: HTMLElement }, language: Language) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      choose(language);
      return;
    }
    const option = event.currentTarget;
    const options = [...(option.parentElement?.querySelectorAll<HTMLElement>('[role="option"]') ?? [])];
    const index = options.indexOf(option);
    const target = {
      ArrowDown: options.at((index + 1) % options.length),
      ArrowUp: options.at(index - 1),
      Home: options.at(0),
      End: options.at(-1),
    }[event.key];
    if (target) {
      event.preventDefault();
      target.focus();
    }
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
    aria-label={placement === 'footer' ? currentLabel : `${currentCode}, ${currentLabel}`}
    onclick={() => (open = !open)}
  >
    {#if placement === 'footer'}
      <Icon name="globe" class="size-4 shrink-0 text-aegis-300" animated />
      <span>{currentName}</span>
      <Icon name="chevronsUpDown" class="size-3.5 shrink-0 opacity-70" stroke={2} />
    {:else}
      <Icon name="globe" class="size-4 shrink-0" />
      <span>{currentCode}</span>
    {/if}
  </Button>
  <Popover
    {id}
    label={translate('language.label')}
    {open}
    from={placement === 'footer' ? 'below' : 'above'}
    onclose={() => (open = false)}
    class={[
      'w-max max-w-[calc(100vw-2rem)] p-1.5',
      placement === 'footer' ? 'bottom-full left-0 mb-2 sm:right-0 sm:left-auto' : 'top-full right-0 mt-2',
    ]}
  >
    <ul
      role="listbox"
      aria-label={translate('language.list')}
      class="-mx-0.5 grid max-h-[min(32rem,calc(100dvh-8rem))] gap-0.5 overflow-y-auto overscroll-contain px-0.5 py-0.5"
    >
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
            'flex h-11 cursor-pointer items-center gap-3 rounded-control px-2.5 text-ui whitespace-nowrap transition md:h-10',
            selected
              ? 'bg-aegis-500/12 text-ink shadow-rim ring-1 ring-aegis-400/35'
              : 'text-ink-soft hover:bg-white/6 hover:text-ink',
          ]}
        >
          <LanguageFlag {language} />
          <span class="flex-1 text-left">{LANGUAGE_DETAILS[language].name}</span>
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
