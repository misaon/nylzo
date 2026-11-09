<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Composer from './composer/Composer.svelte';
  import About from './sections/About.svelte';
  import Compare from './sections/compare/Compare.svelte';
  import Extras from './sections/Extras.svelte';
  import Faq from './sections/Faq.svelte';
  import FinalCall from './sections/FinalCall.svelte';
  import HowItWorks from './sections/how/HowItWorks.svelte';
  import Operations from './sections/Operations.svelte';
  import Promises from './sections/Promises.svelte';
  import Resilience from './sections/resilience/Resilience.svelte';
  import SelfHost from './sections/self-host/SelfHost.svelte';
  import Technology from './sections/Technology.svelte';
  import TrustStrip from './sections/TrustStrip.svelte';
  import WhoSees from './sections/WhoSees.svelte';

  const { translate } = getTranslator();
</script>

<svelte:head>
  <title>{translate('seo.title')}</title>
  <meta name="description" content={translate('seo.description')} />
</svelte:head>

<Composer />
<TrustStrip />
<HowItWorks />
<Promises />
<WhoSees />
<Resilience />
<Extras />
<Technology />
<SelfHost />
<Compare />
<Operations />
<About />
<Faq />
<FinalCall />
