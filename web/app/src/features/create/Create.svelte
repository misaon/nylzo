<script lang="ts">
  import Composer from './composer/Composer.svelte';
  import Compare from './sections/Compare.svelte';
  import Extras from './sections/Extras.svelte';
  import FinalCall from './sections/FinalCall.svelte';
  import HowItWorks from './sections/HowItWorks.svelte';
  import Limits from './sections/Limits.svelte';
  import Promises from './sections/Promises.svelte';
  import Resilience from './sections/Resilience.svelte';
  import SelfHost from './sections/SelfHost.svelte';
  import WhoSees from './sections/WhoSees.svelte';
  import { getI18n } from '../../shared/i18n/i18n.svelte.ts';

  const { t } = getI18n();
</script>

<svelte:head>
  <title>{t('seo.title')}</title>
  <meta name="description" content={t('seo.description')} />
</svelte:head>

<Composer />
<HowItWorks />
<WhoSees />
<Resilience />
<Promises />
<Extras />
<SelfHost />
<Compare />
<Limits />
<FinalCall />
