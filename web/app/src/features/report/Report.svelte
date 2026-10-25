<script lang="ts">
  import ReportForm from '../../shared/report/ReportForm.svelte';
  import { createApi } from '../../shared/api/api.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { reportLink } from '../../shared/report/report.ts';

  const { translate } = getTranslator();
  const api = createApi();
</script>

<section aria-labelledby="report-title">
  <h2 id="report-title">{translate('report.title')}</h2>
  <p>{translate('report.note')}</p>
  <p>{translate('report.privacy')}</p>
  <ReportForm name="report" withLink send={(link, reason) => reportLink(api, location.origin, link, reason)} />
</section>
