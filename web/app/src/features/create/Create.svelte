<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
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

  const { translate } = getTranslator();
</script>

<svelte:head>
  <title>{translate('seo.title')}</title>
  <meta name="description" content={translate('seo.description')} />
</svelte:head>

<Composer />
<HowItWorks />
<Promises />
<WhoSees />
<Resilience />
<Extras />
<SelfHost />
<Compare />
<Limits />
<FinalCall />
