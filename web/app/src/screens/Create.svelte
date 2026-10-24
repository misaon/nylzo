<script lang="ts">
  import Composer from '../components/create/Composer.svelte';
  import Compare from '../components/home/Compare.svelte';
  import Extras from '../components/home/Extras.svelte';
  import FinalCall from '../components/home/FinalCall.svelte';
  import HowItWorks from '../components/home/HowItWorks.svelte';
  import Limits from '../components/home/Limits.svelte';
  import Promises from '../components/home/Promises.svelte';
  import Resilience from '../components/home/Resilience.svelte';
  import SelfHost from '../components/home/SelfHost.svelte';
  import WhoSees from '../components/home/WhoSees.svelte';
  import { getI18n } from '../lib/i18n.svelte.ts';

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
