# Hosting research: EU-hosted, free-tier-first options for a one-time encrypted sharing app

Research date: 2026-10-21. Workload assumed: hundreds of messages per day at most, 5 MB maximum per message, public instance, data at rest (and backups and logs where possible) inside the EU. Budget: 0 CZK preferred, 100 CZK per month maximum.

Exchange rates used for CZK figures: CNB fixing valid for 21 Oct 2026, 24.260 CZK per EUR and 21.593 CZK per USD (https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/, quote: "Validity for 21 Oct 2026"). The ECB reference rate for 20 Oct 2026 was 24.329 CZK per EUR; the ECB page had no value yet for 21 Oct (https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/eurofxref-graph-czk.en.html). At the CNB rate the 100 CZK budget is 4.12 EUR or 4.63 USD. Where a page states a price excluding VAT, the CZK figure "incl. VAT" below adds Czech VAT at 21 %, which is my calculation, not the provider's.

Status legend: VERIFIED means the page was opened and the quote comes from it. PARTIAL means the page was opened but only part of the claim is supported, or the support is indirect. NOT FOUND means no official page stated it; the note says what was tried.

Every page was fetched through a text extractor. Quotes are copied from the extracted text and may differ from the rendered page in spacing or punctuation.

## 1. Cloudflare (Workers Free plan)

Domicile: United States. DPA names "Cloudflare, Inc." at "101 Townsend Street San Francisco, CA 94107 USA" (https://www.cloudflare.com/cloudflare-customer-dpa/, "Version 6.4, effective April 3, 2026"). VERIFIED.

Workers compute

- Requests per day on Free: 100,000. https://developers.cloudflare.com/workers/platform/limits/ (page says "Last updated Oct 12, 2026"). Quote: "100,000/day." Status: VERIFIED. Resets at midnight UTC; over the limit returns Error 1027.
- CPU time per invocation on Free: 10 ms. https://developers.cloudflare.com/workers/platform/pricing/ Quote: "10 milliseconds of CPU time per invocation." Status: VERIFIED. The limits page lists 10 ms for HTTP requests and 10 ms for Cron Triggers.
- Script size: 64 MiB uncompressed, memory 128 MB per isolate, 50 subrequests per invocation, 100 Workers per account. https://developers.cloudflare.com/workers/platform/limits/ Quote: "There is no compressed size limit." Status: VERIFIED.
- Maximum request body on Free: 100 MB, set by the Cloudflare account plan (Free zone plan), not the Workers plan. Same URL. Quote: "Request body size limits depend on your Cloudflare account plan, not your Workers plan." Status: VERIFIED. A 5 MB upload fits.
- Cron Triggers on Free: 5 per account (limits page) and 10 ms CPU per scheduled run (pricing page). https://developers.cloudflare.com/workers/platform/limits/ and https://developers.cloudflare.com/workers/configuration/cron-triggers/ Quote (cron page): "Refer to Workers limits for the maximum number of Cron Triggers per account." Status: VERIFIED for the count; the cron page itself does not say "Free".
- Where Workers run: the data center that received the request; Cloudflare's own Smart Placement moves it automatically and Placement Hints can request a cloud region. https://developers.cloudflare.com/workers/configuration/smart-placement/ Quote: "run in a data center closest to where the request was received". Quote: "Placement Hints let you explicitly specify where your Worker runs". Status: PARTIAL. Smart Placement is listed as available on all plans; the page does not say whether Placement Hints are on Free, and the dedicated placement-hints URL returned 404. There is no statement that a Free Worker can be pinned to the EU. https://developers.cloudflare.com/workers/reference/how-workers-works/ adds only "Workers functions run on Cloudflare's global network."
- Static assets: no charge, no request cap. https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/ Quote: "Requests to static assets are free and unlimited." Quote: "There is no additional cost for storing Assets." Status: VERIFIED. If `run_worker_first` matches, requests over the Free request cap get "a 429 (Too Many Requests) response."
- Pages Free limits (if used instead of Workers assets): 500 builds per month, 20,000 files, 25 MiB per asset, 100 custom domains per project, 100 projects. https://developers.cloudflare.com/pages/platform/limits/ Quote: "The maximum file size for a single Cloudflare Pages site asset is 25 MiB." Status: VERIFIED.
- Rate limiting binding on Free: the binding page has no plan restriction and no price. https://developers.cloudflare.com/workers/runtime-apis/bindings/rate-limit/ Quote: "Rate limits that you define and enforce in your Worker are local to the Cloudflare location". Status: PARTIAL (no plan statement either way; limits are per location, not global).
- Custom domains on Free: the doc requires a Cloudflare zone and states no plan restriction. https://developers.cloudflare.com/workers/configuration/routing/custom-domains/ Quote: "You cannot create a Custom Domain on a hostname with an existing CNAME DNS record or on a zone you do not own." Status: PARTIAL. The routing overview says the workers.dev subdomain "is intended for personal or hobby projects that aren't business-critical" (https://developers.cloudflare.com/workers/configuration/routing/).

Workers KV (Free)

- 100,000 reads, 1,000 writes, 1,000 deletes, 1,000 list operations per day; 1 GB stored. https://developers.cloudflare.com/kv/platform/pricing/ and https://developers.cloudflare.com/kv/platform/limits/ (limits page "Last updated Oct 8, 2026"). Quote: "1,000 writes per day". Status: VERIFIED. Value size limit 25 MiB, one write per second per key. The 1,000 writes per day cap is the binding constraint for this app: each message needs at least one write and one delete.
- KV has no EU storage option. https://developers.cloudflare.com/data-localization/compatibility/ Quote: "Jurisdictional Restrictions (storage) for Workers KV pairs is not supported today." Status: VERIFIED.

R2

- Free tier per month: 10 GB-month storage, 1 million Class A, 10 million Class B, egress free. https://developers.cloudflare.com/r2/pricing/ Quote: "You can use the following amount of storage and operations each month for free." Quote: "The free tier only applies to Standard storage, and does not apply to Infrequent Access storage." Status: VERIFIED.
- R2 needs a subscription added through checkout. https://developers.cloudflare.com/r2/get-started/ Quote: "You need a Cloudflare account with an R2 subscription." Quote: "Complete the checkout flow to add an R2 subscription to your account." Status: VERIFIED for the subscription step. Whether a payment card is mandatory: NOT FOUND on official pages; third-party guides (search only, not opened) say a card or PayPal is requested at activation.
- EU jurisdiction for a bucket: guarantees object storage location, set at creation, cannot be changed. https://developers.cloudflare.com/r2/reference/data-location/ Quote: "Jurisdictional Restrictions guarantee objects in a bucket are stored within a specific jurisdiction." Quote: "Once an R2 bucket is created, the jurisdiction cannot be changed." Status: VERIFIED.
- What the jurisdiction does not cover: the page lists only Logpush as a service that does not interact with jurisdiction buckets; it says nothing about metadata, dashboard analytics or caching. Status: PARTIAL. The DLS compatibility page notes for R2 that "R2 Dashboard Metrics and Analytics are populated." under Customer Metadata Boundary, which implies metrics exist outside the bucket's jurisdiction unless CMB is set.
- Location hints are not a guarantee. Same URL. Quote: "Location Hints are a best effort and not a guarantee". Status: VERIFIED.

D1

- Free limits: 10 databases, 500 MB per database, 5 GB per account, 5 million rows read per day, 100,000 rows written per day. https://developers.cloudflare.com/d1/platform/limits/ and https://developers.cloudflare.com/d1/platform/pricing/ Quote: "500 MB (Free)". Status: VERIFIED.
- Location hint is best effort. https://developers.cloudflare.com/d1/configuration/data-location/ ("Last updated Oct 2, 2026"). Quote: "Providing a location hint does not guarantee that D1 runs in your preferred location." Status: VERIFIED.
- D1 jurisdiction (eu, us, fedramp) exists on the D1 page but the DLS compatibility page says it is unsupported. Same D1 URL. Quote: "The jurisdiction constraint only controls where the database itself runs and persists data." Quote: "Jurisdictions can only be set on database creation and cannot be added or updated after the database exists." Compatibility page quote (footnote 20): "Jurisdictional Restrictions (data location / storage) options are not supported today." Status: PARTIAL (two official pages disagree; the D1 page is dated 2 Oct 2026 and is the more specific one).

Durable Objects

- Available on Free, SQLite backend only. https://developers.cloudflare.com/durable-objects/platform/pricing/ Quote: "Durable Objects are available both on Workers Free and Workers Paid plans." Quote: "Workers Free plan can only create and access SQLite-backed Durable Objects." Status: VERIFIED.
- Free limits: 100,000 requests per day, 13,000 GB-s per day, 5 million SQLite rows read per day, 100,000 rows written per day, 5 GB stored. Same URL. Quote: "If you exceed any one of the free tier limits, further operations of that type will fail with an error." Status: VERIFIED.
- Jurisdiction "eu": restricts where the object runs and stores data; access from anywhere; the object ID is logged outside. https://developers.cloudflare.com/durable-objects/reference/data-location/ Quote: "The jurisdiction constraint only controls where the Durable Object itself runs and persists data." Quote: "will be logged outside of the specified jurisdiction for billing and debugging purposes." Quote: "Workers may still access Durable Objects constrained to a jurisdiction from anywhere in the world." Status: VERIFIED. Supported values: eu, us, fedramp. Location hints: "Hints are a best effort and not a guarantee." The page does not say whether jurisdictions need a paid plan.

Logs and analytics

- Workers Logs on Free: 200,000 log events per day, 3 days retention. https://developers.cloudflare.com/workers/observability/logs/workers-logs/ Quote: "200,000 per day". Quote: "3 Days". Status: VERIFIED. The page says only "Data is written to your Cloudflare Account" about location. It also says "Beginning December 1, 2026, Workers Logs will use" Cloudflare Observability pricing.
- Where logs and analytics live by default: Cloudflare's core data centers globally, unless Customer Metadata Boundary is configured. https://developers.cloudflare.com/data-localization/metadata-boundary/ Quote: "by default no boundary is applied and logs may be stored in Cloudflare's core data centers globally." Status: VERIFIED. CMB supports "eu" and "us" only; the page states no plan requirement, but the DLS overview calls the whole suite an Enterprise add-on (next bullet).
- Workers logs and analytics are tied to the US region under CMB. https://developers.cloudflare.com/data-localization/compatibility/ Quote (Workers footnote 27): "Logs / Analytics not available outside US region when using Customer Metadata Boundary. Use Logpush instead." Status: VERIFIED. The same footnote applies to Durable Objects and Workers Analytics Engine.
- GDPR page: metadata and logs are processed in the US and Europe. https://www.cloudflare.com/trust-hub/gdpr/ Quote: "Metadata about this activity is processed on behalf of our customers in our data centers in the United States and Europe." Status: VERIFIED. Cloudflare says it is "headquartered in the United States" and "has also certified our compliance with the EU-U.S. Data Privacy Framework."

Data Localization Suite and Regional Services

- DLS is Enterprise only. https://developers.cloudflare.com/data-localization/ Quote: "Enterprise-only paid add-on". Status: VERIFIED.
- Regional Services is an Enterprise add-on and covers Workers execution. https://developers.cloudflare.com/data-localization/regional-services/ Quote: "Regional Services is an Enterprise add-on." Quote: "Running Cloudflare Workers scripts" (listed among services that run inside the configured region). Status: VERIFIED. On Free there is no way to keep Worker execution or TLS termination inside the EU.
- DPA on transfers: https://www.cloudflare.com/cloudflare-customer-dpa/ Quote: "the parties anticipate that Cloudflare (and its sub-Processors) may process outside of the European Economic Area". Quote: "Cloudflare complies with the Data Privacy Framework". Status: VERIFIED.

Cost after free: Workers Paid is "minimum charge of $5 USD per month for an account" (pricing page), about 108 CZK, which is over budget. VERIFIED.

## 2. Scaleway (France)

Domicile: France. https://www.scaleway.com/en/legal-notice/ Quote: "Registered office: 8, rue de la Ville-l'Évêque, 75008 Paris, France". Quote: "SIREN: 433 115 904 RCS Paris". https://www.scaleway.com/en/about-us/ Quote: "A subsidiary of the iliad Group." Status: VERIFIED.

- Serverless Containers free tier: 400,000 GB-s and 200,000 vCPU-s per account per month; no per-request charge is listed for containers. https://www.scaleway.com/en/pricing/serverless/ Quote: "after a 400 000 GB-s Free Tier per account and per month". Quote: "after a 200 000 vCPU-s Free Tier per account and per month". Prices stated as "Prices before tax." in EUR. Status: VERIFIED. The FAQ gives the example that 500 mvCPU / 1024 MB equals about 111 hours per month inside the free tier (https://www.scaleway.com/en/docs/serverless-containers/faq/). A container that scales to zero between requests stays well inside this at the expected load.
- Serverless Functions free tier: 1,000,000 requests and 400,000 GB-s per account per month. Same pricing URL. Quote: "after 1 000 000 requests Free Tier per account and per month". Status: VERIFIED. Functions limit request payload to 6 MiB (https://www.scaleway.com/en/docs/serverless-functions/reference-content/functions-limitations/, quote: "6 MiB"), which leaves little headroom over a 5 MB file once encoded; Containers are the safer choice.
- Serverless Jobs free tier: same 400,000 GB-s and 200,000 vCPU-s per month. Same pricing URL. Quote: "Prices before taxes". Status: VERIFIED.
- Serverless Containers limits: request timeout 10 s to 60 min, 70 to 6000 mvCPU, 128 to 12228 MB, scale to zero after 15 minutes, 200 instances max. https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/ Quote: "Use Serverless Jobs for tasks up to 24h." Status: VERIFIED. Maximum request body size for Containers: NOT FOUND (limitations page, FAQ and a web search; only the Functions 6 MiB limit is documented).
- Scale to zero and cold starts: a container is kept warm after each deploy; nothing wakes it except traffic. https://www.scaleway.com/en/docs/serverless-containers/faq/ Quote: "Health checks will not wake up Serverless Containers if no instance is up." Status: VERIFIED. The product page describes "Your service scales from zero to 200 on demand" (https://www.scaleway.com/en/serverless-containers/).
- Object Storage: no monthly free storage; 75 GB free egress per month; Standard One Zone €0.00803 per GB-month, Multi-AZ €0.01606 per GB-month, before tax. https://www.scaleway.com/en/pricing/storage/ Quote: "75GB free every month then €0.01/GB /MONTH". Status: VERIFIED. The FAQ describes a one-time 90-day trial: "a total 750 GB of free storage across the Standard Multi-AZ and Standard One Zone classes" (https://www.scaleway.com/en/docs/object-storage/faq/). At 5 MB per message and short retention, storage cost rounds to a few cents.
- Serverless SQL Database: no free tier; €0.13752 per vCPU-hour, €0.000272 per GB-hour (about €0.20 per GB-month), 7 days of backups included, no compute charge at 0 vCPU idle. https://www.scaleway.com/en/pricing/managed-databases/ and https://www.scaleway.com/en/docs/serverless-sql-databases/faq/ Quote: "You will not be billed if your database is configured with 0 vCPU as a minimum and is in idle status." Status: VERIFIED. Paris only (see next bullet).
- Regions: Paris (fr-par), Amsterdam (nl-ams), Warsaw (pl-waw), Milan listed. https://www.scaleway.com/en/docs/account/reference-content/products-availability/ Containers: "Available across the region" in all four; Serverless SQL Database "Available across the region" in Paris only. Status: VERIFIED.
- Payment card: not needed to open the account, needed to order resources; a €1 authorisation is taken and refunded; prepaid virtual cards refused. https://www.scaleway.com/en/docs/account/how-to/create-an-account/ Quote: "Ordering Scaleway resources requires a valid credit card." Quote: "Virtual prepaid cards are not accepted as a payment method." Status: VERIFIED.
- "Always free" wording: NOT FOUND. The product page says "Serverless Containers includes a generous monthly Free Tier." and the pricing page states the allowance "per account and per month"; nothing says "always free" or "free forever".

Cost after free: compute usage above the monthly allowance is billed at €0.000002 per GB-s and €0.00001 per vCPU-s before tax; Object Storage a few cents at this volume. Realistically 0 to under 1 EUR per month, plus VAT.

## 3. Oracle Cloud Always Free

Domicile: United States. https://en.wikipedia.org/wiki/Oracle_Corporation Quote: "headquartered in Austin, Texas". Status: PARTIAL (oracle.com/corporate returned HTTP 403; Wikipedia used as a secondary source).

- Compute: two AMD micro VMs plus Ampere A1 up to 2 OCPUs and 12 GB. https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm Quote: "All tenancies get up to two Always Free VM instances using the VM.Standard.E2.1.Micro shape". Quote: "For Always Free tenancies, this is equivalent to 2 OCPUs and 12 GB of memory." Status: VERIFIED. The A1 figure is "the first 1,500 OCPU hours and 9,000 GB hours per month"; a blog post dated 9 July 2026 says this was halved from 4 OCPU/24 GB on 15 June 2026 (anecdotal, see below).
- Block storage: 200 GB total including boot volumes, five backups; Object Storage 20 GB; outbound transfer 10 TB per month. Same URL. Quote: "As part of your Always Free resources, you get 10 TB per month of outbound data." Status: VERIFIED.
- Idle reclamation policy. Same URL. Quote: "Idle Always Free compute instances may be reclaimed by Oracle." Criteria over a 7-day period: "CPU utilization for the 95th percentile is less than 20%", "Network utilization is less than 20%", "Memory utilization is less than 20% (applies to A1 shapes only)". Status: VERIFIED. A low-traffic sharing service will meet all three idle criteria, so reclamation is a real risk without a keep-busy workaround.
- Home region restriction. Same URL and https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier.htm Quote: "You must create the Always Free compute instances in your home region." Quote: "During sign-up, choose the home region carefully." Status: VERIFIED. The page does not say whether the home region can be changed later (NOT FOUND).
- Credit card. https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier.htm Quote: "most users need a mobile phone number and a credit card to create an account." Status: VERIFIED. https://www.oracle.com/cloud/free/ and its FAQ returned HTTP 403.
- EU regions available: Paris eu-paris-1, Marseille eu-marseille-1, Frankfurt eu-frankfurt-1, Milan eu-milan-1, Turin eu-turin-1, Amsterdam eu-amsterdam-1, Madrid eu-madrid-1 and eu-madrid-3, Stockholm eu-stockholm-1, Zurich eu-zurich-1 (Switzerland, outside the EU), plus Jovanovac (Serbia, outside the EU). https://docs.oracle.com/en-us/iaas/Content/General/Concepts/regions.htm Status: VERIFIED. Whether every listed region can be chosen as a free-tier home region at sign-up is not stated on that page.
- Anecdotal capacity reports (not official, treat as hearsay): a blog post dated 9 July 2026 says "an 'Out of host capacity' error is not unusual, especially in high-demand regions" and that Oracle "halved Always Free Ampere A1 limits from 4 OCPU/24GB RAM to 2 OCPU/12GB RAM, effective June 15, 2026" (https://braindetox.kr/ja/posts/oracle_always_free_backend_2026.html, translated). Oracle Community forum pages tagged "arm" timed out three times, so forum reports about eu-frankfurt-1 instances being disabled in August 2026 (seen only in search snippets) are NOT VERIFIED.

Cost after free: 0 if you stay inside Always Free. Backups and logs stay in the home region if you keep them in the tenancy, which is the one setup here where everything can sit in a single EU region, but the provider is US-domiciled.

## 4. Fly.io

Domicile: United States. https://fly.io/legal/terms-of-service/ Quote: "Fly.io, Inc." Quote: "governed and interpreted by and under the laws of the State of California." Status: VERIFIED.

- No free allowance in 2026. https://docs.fly.io/about/pricing Quote: "No. New organizations don't have a free tier or a monthly free usage allowance." Quote: "New organizations use Pay As You Go pricing." Status: VERIFIED. New accounts get a trial of up to 2 hours of Machine runtime or 7 days.
- Cheapest VM: shared-cpu-1x 256 MB at $2.19 per month (47.3 CZK); 512 MB at $3.69 (79.7 CZK). Same URL. Status: VERIFIED. Volumes $0.15 per GB-month (3.2 CZK per GB). Egress from Europe $0.02 per GB.
- EU regions: ams Amsterdam, arn Stockholm, cdg Paris, fra Frankfurt, lhr London (UK, outside the EU). https://docs.fly.io/reference/regions Status: VERIFIED. waw, mad, otp are not on the page (NOT FOUND).

Cost after free: a 256 MB Machine plus a 1 GB volume is about $2.34, roughly 50.5 CZK per month before tax, so inside budget but not free. Fly's docs do not state where logs are stored.

## 5. Render

Domicile: United States. https://render.com/terms and https://render.com/terms-update are JavaScript-rendered and returned no body text; a search snippet names "Render Services, Inc." Status: NOT FOUND on an opened page.

- Free web services spin down after 15 minutes idle; 750 free instance hours per workspace per month. https://render.com/docs/free Quote: "goes 15 minutes without receiving any inbound traffic". Status: VERIFIED. The filesystem is wiped on restart.
- Persistent disks are not available on free. Same URL and https://render.com/docs/disks Quote: "You can attach a persistent disk to a paid Render web service, private service, or background worker." Status: VERIFIED.
- Frankfurt region. https://render.com/docs/regions Quote: "Frankfurt, Germany". Status: VERIFIED.
- Free Postgres expires after 30 days, 1 GB, no backups. https://render.com/docs/free Quote: "expire 30 days after creation." Status: VERIFIED.
- Free Key Value is in memory only. Same URL. Quote: "do not continually persist their state to disk". Status: VERIFIED.

Cost after free: Starter web service $7 per month, 512 MB, less than 1 CPU (151.2 CZK); disk $0.25 per GB-month. https://render.com/pricing.md Quote: "$0.25 per GB per month". Status: VERIFIED. Over budget once paid.

## 6. Koyeb

Domicile: France (Paris area) per search results only; https://www.koyeb.com/about does not name a city and the legal pages returned 404. Status: NOT FOUND on an opened page. A CFNews item from July 2026 in search results says Mistral AI is acquiring Koyeb (not opened).

- Free instance: 0.1 vCPU, 512 MB RAM, 2 GB SSD, one per organization, Frankfurt or Washington only, no volumes, sleeps after one hour idle. https://www.koyeb.com/docs/reference/instances Quote: "Each organization is limited to one Free Instance." Quote: "They can be deployed in only one region, either Frankfurt or Washington, D.C." Status: VERIFIED.
- Plan structure: the pricing page lists only Pro at "$29/mo", Scale at "$299/mo" and Enterprise, and the words "Hobby", "Starter", "payment method" and "free Instance" do not appear on it. https://www.koyeb.com/pricing Status: PARTIAL. The free instance is still documented, but whether it can be used without the $29 Pro plan or a card is not stated on any page I could open (the pro-plan-trial FAQ at https://koyeb.com/docs/faqs/pro-plan-trial only says "Start with a free Instance: 512MB of RAM, 0.1 vCPU, and 2GB of SSD."). Community thread on card requirement redirected to a login page. A third-party table (search snippet, not opened) claims the card-free Hobby plan was removed in February 2026.
- Frankfurt region code FRA, Paris PAR. https://www.koyeb.com/docs/reference/regions Quote: "FRA: Frankfurt, Germany". Status: VERIFIED.

Cost after free: Eco-nano $1.61 per month (34.8 CZK) or Nano $2.68 (57.9 CZK), on top of whatever plan fee applies; if the $29 Pro plan is mandatory the real floor is about 626 CZK.

## 7. Vercel Hobby

Domicile: United States. https://vercel.com/legal/terms Quote: "Vercel Inc." Quote: "governed by the laws of the State of California". Status: VERIFIED.

- Non-commercial restriction. https://vercel.com/docs/limits/fair-use-guidelines ("last_updated: 2026-09-14") Quote: "Hobby teams are restricted to non-commercial personal use only. All commercial usage of the platform requires either a Pro or Enterprise plan." Status: VERIFIED. Donations are allowed: "Asking for Donations does not fall under commercial usage." An open-source public instance without ads or payments fits.
- Function region on Hobby: one region, selectable, default iad1 (US). https://vercel.com/docs/functions/configuring-functions/region ("last_updated: 2026-08-11") Quote: "Hobby | Single region". Quote: "By default, Vercel Functions execute in Washington, D.C., USA (iad1) for all new projects". Status: VERIFIED. fra1 is Frankfurt (https://vercel.com/docs/regions, "fra1 | eu-central-1 | Frankfurt, Germany").
- Hobby included usage: 1,000,000 function invocations, 100 GB Fast Data Transfer, 4 CPU-hours, 360 GB-hours, 1 hour of runtime logs, 100 cron jobs. https://vercel.com/docs/plans/hobby ("last_updated: 2026-09-14"). Quote: "Runtime Logs | 1 hour of logs". Status: VERIFIED.
- Vercel Blob on Hobby: 1 GB storage, 10,000 simple ops, 2,000 advanced ops, 10 GB transfer per month; over the limit, access stops for 30 days. https://vercel.com/docs/vercel-blob/usage-and-pricing ("last_updated: 2026-09-23"). Quote: "you will not be able to access Vercel Blob if limits are exceeded." Status: VERIFIED.
- Blob data location: one region chosen at store creation out of the 19 Vercel regions (fra1 included), on Amazon S3, cannot be changed. https://vercel.com/docs/vercel-blob Quote: "Each store lives in one region that you choose". Quote: "Vercel Blob leverages Amazon S3 as its underlying storage infrastructure". Status: VERIFIED. CDN caching of blobs is global ("caches your files close to your users").
- Cron on Hobby: once per day with up to 59 minutes of drift. https://vercel.com/docs/cron-jobs/usage-and-pricing ("last_updated: 2026-07-15"). Quote: "Hobby accounts are limited to cron jobs that run once per day." Status: VERIFIED. Expiry sweeps must therefore run lazily on request, not by cron.

Cost after free: Pro $20 per user per month (431.9 CZK), over budget.

## 8. Supabase Free

Domicile: Singapore by default, US entity for AWS Marketplace purchases, California governing law. https://supabase.com/terms Quote: "a Singapore corporate entity". Status: VERIFIED.

- Free plan: 500 MB database, 1 GB file storage, 5 GB egress, 2 active projects, pauses after a week idle. https://supabase.com/pricing Quote: "Free projects are paused after 1 week of inactivity." Quote: "Limit of 2 active projects." Status: VERIFIED. Restore window after pausing is one year (https://supabase.com/docs/guides/platform/upgrading, quote: "users have a 1-year window to restore the project").
- Frankfurt region. https://supabase.com/docs/guides/platform/regions Quote: "Central EU (Frankfurt), eu-central-1". Status: VERIFIED. The page warns: "Region selection is a data-location control, not proof of regulatory compliance."

Cost after free: Pro "from $25/month" (539.8 CZK).

## 9. Neon, Upstash, Turso (brief)

- Neon Free: 1 GB per project, 20 GB per account, 100 CU-hours per project per month, scale to zero after 5 minutes (cannot be disabled). https://neon.com/pricing and https://neon.com/docs/introduction/plans Quote: "1 GB/project, 20 GB account total." Status: VERIFIED. Frankfurt is "aws-eu-central-1" (https://neon.com/docs/introduction/regions); the regions page does not say whether Free projects can pick it (PARTIAL). Entity: "Databricks, Inc., the parent company of Neon, LLC" (https://neon.com/terms-of-service), US.
- Upstash Redis Free: 500,000 commands per month, 250 MB (table says 256 MB), 10 GB bandwidth. https://upstash.com/pricing/redis Quote: "Free tier includes 250MB data size and 500K commands per month." Status: VERIFIED. Primary region list includes eu-central-1 and eu-west-1 (https://upstash.com/docs/devops/developer-api/redis/create_database_global); whether Free databases can be placed there is not stated (PARTIAL). Entity: Upstash terms PDF was unreadable; domicile NOT FOUND on an opened page (US company by common knowledge).
- Turso Free: 100 databases, 5 GB, 500 million rows read, 10 million rows written per month. https://turso.tech/pricing Quote: "Start free, no credit card required." Status: VERIFIED. Locations: the API sample lists only "aws-eu-west-1": "AWS EU West (Ireland)" among EU options (https://docs.turso.tech/api-reference/locations/list); the group CLI says "Turso will automatically detect the closest region and use that as the primary region." (https://docs.turso.tech/cli/group/create). Frankfurt on Turso: NOT FOUND on opened pages. Entity page returned 404; US company (PARTIAL).

## 10. Deno Deploy and Netlify (brief)

- Deno Deploy Free: 1 million requests, 20 GiB egress, 10 hours active CPU per month, 10 apps. https://deno.com/deploy/pricing Status: VERIFIED. The current platform has two regions and an "eu" option: https://docs.deno.com/deploy/migration_guide/ Quote: "The new Deploy currently has 2 regions, US (us) and Europe (eu)". CLI: "Allowed values: us, eu, global" (https://docs.deno.com/runtime/reference/cli/deploy/). Status: VERIFIED. Deno KV is not EU-resident: "Data written to KV is therefore stored in, and transits through the US." and "For workloads that require EU-only data residency or strict GDPR guarantees, Deno KV is not suitable in its current form." (https://docs.deno.com/deploy/reference/deno_kv/). Entity: "Deno Land Inc." (footer at https://deno.com/terms), US.
- Netlify Free: 300 credits per month, hard limit, no charge. https://docs.netlify.com/manage/accounts-and-billing/billing/billing-for-credit-based-plans/credit-based-pricing-plans/ Quote: "$0/month, forever, for 300 credits/month with a hard limit and no auto recharge option." Rates: "Web Bandwidth: 1 GB transferred → 20 credits.", "Functions compute: 1 GB-hour → 10 credits." Status: VERIFIED. 300 credits is about 15 GB of bandwidth with nothing left for compute. No region choice is documented. Entity: "Netlify, Inc." at "101 2nd Street, San Francisco, CA 94105" (https://www.netlify.com/legal/terms-of-use/), US.

## 11. Paid fallback (EU-domiciled)

- Hetzner Cloud CX23: 2 vCPU, 4 GB, 40 GB NVMe, 20 TB traffic, "€ 5.99" per month "excl. VAT", available at FSN, NBG, HEL. https://www.hetzner.com/cloud/cost-optimized/ Status: VERIFIED for the price; every type on the page carried a "Currently not available" label in the extracted text, which may be a page artifact, so check the console before relying on stock. The page label reads "Price incl. IPv4"; the separate IPv4 price is "Cloud Primary IPv4 € 0.50 ($ 0.60) monthly" (https://docs.hetzner.com/general/others/ipv4-pricing/). CZK: 145.3 excl. VAT, about 175.8 incl. 21 % VAT. CAX11 (Ampere, 2 vCPU, 4 GB) is €6.49 excl. VAT, 157.4 CZK, about 190.5 incl. VAT. Both are over the 100 CZK budget.
- Hetzner Object Storage: base €6.49 per month excl. VAT for 1 TB stored and 1 TB egress. https://www.hetzner.com/storage/object-storage/ Quote: "We charge a base price of € 6.49 per month, excl. VAT per month for using Object Storage." Status: VERIFIED. 157.4 CZK excl. VAT; locations FSN1, HEL1, NBG1. Over budget on its own.
- Hetzner Storage Box BX11: 1 TB, "€ 3.20 per month, excl. VAT", Germany or Finland. https://www.hetzner.com/storage/storage-box/ Status: VERIFIED. 77.6 CZK excl. VAT, about 93.9 incl. VAT. It is SFTP/SMB storage, not an S3 API, so it needs a server in front of it.
- Hetzner domicile: "Hetzner Online GmbH", Gunzenhausen, Germany (https://www.hetzner.com/legal/legal-notice/). VERIFIED.
- Scaleway STARDUST1-S: 1 vCPU, 1 GB, €0.0006 per hour before tax (about €0.44 per month at 730 h), storage and IPv4 excluded. https://www.scaleway.com/en/pricing/virtual-instances/ Quote: "Prices before tax." Status: VERIFIED for the listed price. About 10.6 CZK per month excl. VAT plus storage and an IPv4 if needed; availability is not stated on the page. DEV1-S: 2 vCPU, 2 GB, €0.00898 per hour (about €6.55 per month, 159 CZK), over budget.
- OVHcloud VPS-1: 2 vCores, 4 GB, 40 GB NVMe, "From €3.81 ex. VAT/month" with the configurator preset to 12 months upfront. https://www.ovhcloud.com/en-ie/vps/ Status: PARTIAL (no month-to-month price shown; datacenter list not in the extracted text; the Czech site URL returned 404). 92.4 CZK excl. VAT, about 111.8 incl. VAT. OVH is "a French company" trading as "OVH SAS" (https://www.ovhcloud.com/en-ie/about-us/). VERIFIED.
- Contabo Cloud VPS 4: 4 vCPU, 8 GB, 100 GB SSD, "€4.40/Month" on a 24-month term; the page says the figure is "the effective monthly rate for a 24-month subscription including applicable taxes". https://contabo.com/en/vps/ Status: PARTIAL (a second figure of €5.50 appears on the card without a readable label). 106.7 CZK. Entity "Contabo GmbH", Munich, Germany (https://contabo.com/en/about-us/). VERIFIED.

## 12. Domicile summary and EU-only backup and log statements

- Cloudflare: US (San Francisco). No EU-only statement for logs on Free; CMB and Regional Services are Enterprise.
- Scaleway: France (Paris), iliad Group. Serverless SQL backups are "stored 7 days" in the same product; the pricing and docs pages do not say where logs are kept. No explicit EU-only logs statement found.
- Oracle: US (Austin). Always Free resources live in the home region; no explicit log-location statement found.
- Fly.io: US (California). No EU log statement.
- Render: US (page text unavailable). No EU log statement.
- Koyeb: France per search results, unverified on an opened page. No EU log statement.
- Vercel: US (California). Blob region chosen per store; runtime logs location not stated.
- Supabase: Singapore entity (US entity via AWS Marketplace). Data region chosen per project; log location not stated.
- Neon: US (Databricks). Upstash: not verified (US). Turso: not verified (US). Deno: US. Netlify: US.
- Hetzner: Germany. OVHcloud: France. Contabo: Germany. These three publish EU data-center locations for the product itself; none of the opened pages makes a separate statement about log storage.

No provider page I opened makes an explicit "backups and logs stay in the EU" promise for a free product. The closest are self-managed options (Oracle home region, Hetzner, Scaleway instances) where you control where backups and logs go.

## Comparison table

| Provider | Product | Free allowance | Hard limits that matter here | EU data-at-rest guarantee | Compute location | Logs location | Monthly cost after free | Domicile |
|---|---|---|---|---|---|---|---|---|
| Cloudflare | Workers + R2 (+ DO or D1) | 100k req/day, 10 ms CPU; R2 10 GB, 1M A / 10M B ops; DO 100k req/day, 5 GB; D1 5 GB; KV 1k writes/day | 100 MB body; KV writes 1k/day (too low); R2 needs subscription checkout; cron 5; logs 3 days | Partial: R2 and DO jurisdiction "eu" guarantee storage; D1 jurisdiction page says yes, DLS page says no; KV no | Nearest data center worldwide; no EU pinning on Free | Core data centers globally; Workers logs/analytics US-bound under CMB | $5 (108 CZK) Workers Paid | US |
| Scaleway | Serverless Containers + Object Storage | 400k GB-s + 200k vCPU-s/month; 75 GB egress | Body size for Containers not documented (Functions 6 MiB); 15 min scale-to-zero; card required; storage billed from first byte | Yes for the region you pick (fr-par, nl-ams, pl-waw); no separate log statement | Chosen EU region | Not stated | ~0 to under 1 EUR + VAT | France |
| Oracle | Always Free compute + block storage | 2 micro VMs or A1 2 OCPU/12 GB, 200 GB block, 20 GB object, 10 TB egress | Idle reclamation (<20 % CPU, network, memory over 7 days); home region only; card at sign-up; capacity anecdotes | Yes if home region is EU (self-managed) | Home region | Wherever you put them (self-managed) | 0 | US |
| Fly.io | Machines + volumes | None (trial only) | Pay as you go from day one | Yes for chosen region (ams, fra, cdg, arn) | Chosen region | Not stated | $2.19 to $2.34 (47 to 51 CZK) | US |
| Render | Free web service | 750 h/month | 15 min spin-down; no disk; Postgres expires 30 days; KV in-memory | Yes for Frankfurt (no persistence on free) | Frankfurt selectable | Not stated | $7 (151 CZK) | US |
| Koyeb | Free instance | 1 instance, 0.1 vCPU, 512 MB, 2 GB SSD | Sleeps after 1 h; no volumes; plan/card status unclear | Yes for Frankfurt | Frankfurt or Washington | Not stated | $1.61 to $2.68 + possible $29 plan | France (unverified) |
| Vercel | Hobby functions + Blob | 1M invocations, 1 GB Blob, 10 GB Blob transfer | Non-commercial only; cron once/day; 1 h logs; Blob stops for 30 days when exceeded | Yes for Blob store in fra1; functions single region fra1 | fra1 selectable | Not stated | $20 (432 CZK) Pro | US |
| Supabase | Free Postgres + Storage | 500 MB DB, 1 GB storage, 5 GB egress | Pauses after 1 week idle; 2 projects | Yes for eu-central-1 | Frankfurt selectable | Not stated | $25 (540 CZK) | Singapore |
| Neon | Free Postgres | 1 GB/project, 100 CU-h | Scale to zero 5 min | Frankfurt region exists; Free eligibility not stated | aws-eu-central-1 | Not stated | Pay as you use | US |
| Upstash | Free Redis | 500k commands, 250 MB | Region on free not stated | eu-central-1 exists | n/a | Not stated | Pay as you go | US (unverified) |
| Turso | Free SQLite | 5 GB, 500M reads | Only Ireland seen as EU location | Partial | n/a | Not stated | Pay as you go | US (unverified) |
| Deno Deploy | Free | 1M req, 20 GiB egress, 10 CPU-h | KV is US-only | "eu" region for apps; KV no | eu selectable | Not stated | Not researched | US |
| Netlify | Free | 300 credits (~15 GB bandwidth) | No region choice documented | No | Not stated | Not stated | $9 (194 CZK) Personal | US |
| Hetzner | CX23 / Object Storage / BX11 | None | Stock labels on page | Yes (FSN, NBG, HEL) | Chosen DC | Self-managed | €5.99 / €6.49 / €3.20 excl. VAT (145 / 157 / 78 CZK) | Germany |
| Scaleway | STARDUST1-S | None | Storage and IPv4 extra; availability not stated | Yes | Chosen DC | Self-managed | ~€0.44 excl. VAT (11 CZK) + extras | France |
| OVHcloud | VPS-1 | None | 12-month upfront price | Yes (EU DCs, list not extracted) | Chosen DC | Self-managed | €3.81 excl. VAT (92 CZK) | France |
| Contabo | Cloud VPS 4 | None | 24-month term for the headline price | Yes (EU group) | Chosen DC | Self-managed | €4.40 (107 CZK) | Germany |

## Decision-relevant summary

- Only Scaleway Serverless Containers and Oracle Always Free offer a 0 CZK path with an EU operator or an EU-only region for both compute and storage; Scaleway is the only one of the two that is EU-domiciled.
- Cloudflare Free can keep message bodies in the EU through R2 or Durable Objects "eu" jurisdictions, but Worker execution, logs and analytics are not EU-bound on Free, and Regional Services and the metadata boundary are Enterprise only.
- Cloudflare KV's 1,000 writes per day on Free rules it out as the message store; R2 (1 million Class A ops) or Durable Objects (100,000 rows written per day) fit the load.
- The D1 "eu" jurisdiction is documented on the D1 page (2 Oct 2026) but the DLS compatibility page still says D1 jurisdictions are unsupported; treat D1 EU residency as unconfirmed until the two pages agree.
- Scaleway requires a real payment card (a €1 authorisation, no prepaid virtual cards) and bills Object Storage from the first byte, so the realistic cost is a few cents to under 1 EUR per month, inside the 100 CZK ceiling with wide margin.
- Scaleway documents no request body limit for Containers; Functions cap at 6 MiB. Test a 5 MB upload on Containers before committing, or chunk uploads client-side.
- Oracle's idle-reclamation rule (all of CPU, network and memory under 20 % for 7 days) will trigger on a low-traffic service; the A1 allowance was also cut to 2 OCPU/12 GB in 2026 and capacity errors are widely reported (anecdotal).
- Fly.io has no free tier in 2026; the smallest EU Machine with a 1 GB volume is about 51 CZK per month, inside budget.
- Render, Koyeb and Vercel Hobby give a free Frankfurt region but no free persistent storage suited to 5 MB payloads: Render disks are paid-only, Koyeb free instances cannot use volumes, Vercel Blob stops for 30 days once 1 GB or 10 GB transfer is exceeded.
- Vercel Hobby is non-commercial only (donations allowed), cron runs once a day, and runtime logs last one hour.
- Among paid EU hosts, Hetzner CX23 (€5.99 excl. VAT, about 176 CZK incl. VAT) and Hetzner Object Storage (€6.49 base) exceed 100 CZK; OVH VPS-1 (€3.81 excl. VAT, 12-month upfront) and Scaleway Stardust (about €0.44 before storage and IPv4) are the EU-domiciled options under budget.
- No opened page from any provider promises that backups and logs stay in the EU for a free product; that promise is only achievable on self-managed setups (Oracle EU home region, Hetzner, Scaleway, OVH) where you choose where logs and backups are written.

## URLs opened

Cloudflare
- https://developers.cloudflare.com/workers/platform/limits/
- https://developers.cloudflare.com/workers/platform/pricing/
- https://developers.cloudflare.com/kv/platform/limits/
- https://developers.cloudflare.com/kv/platform/pricing/
- https://developers.cloudflare.com/r2/pricing/
- https://developers.cloudflare.com/r2/reference/data-location/
- https://developers.cloudflare.com/r2/get-started/
- https://developers.cloudflare.com/d1/platform/limits/
- https://developers.cloudflare.com/d1/platform/pricing/
- https://developers.cloudflare.com/d1/configuration/data-location/
- https://developers.cloudflare.com/durable-objects/platform/pricing/
- https://developers.cloudflare.com/durable-objects/reference/data-location/
- https://developers.cloudflare.com/workers/observability/logs/workers-logs/
- https://developers.cloudflare.com/workers/configuration/cron-triggers/
- https://developers.cloudflare.com/workers/static-assets/
- https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/
- https://developers.cloudflare.com/pages/platform/limits/
- https://developers.cloudflare.com/workers/runtime-apis/bindings/rate-limit/
- https://developers.cloudflare.com/workers/configuration/routing/custom-domains/
- https://developers.cloudflare.com/workers/configuration/routing/
- https://developers.cloudflare.com/workers/configuration/smart-placement/
- https://developers.cloudflare.com/workers/configuration/placement-hints/ (404)
- https://developers.cloudflare.com/workers/reference/how-workers-works/
- https://developers.cloudflare.com/data-localization/
- https://developers.cloudflare.com/data-localization/regional-services/
- https://developers.cloudflare.com/data-localization/metadata-boundary/
- https://developers.cloudflare.com/data-localization/compatibility/
- https://www.cloudflare.com/cloudflare-customer-dpa/
- https://www.cloudflare.com/trust-hub/gdpr/

Scaleway
- https://www.scaleway.com/en/pricing/serverless/
- https://www.scaleway.com/en/pricing/storage/
- https://www.scaleway.com/en/pricing/managed-databases/
- https://www.scaleway.com/en/pricing/virtual-instances/
- https://www.scaleway.com/en/serverless-containers/
- https://www.scaleway.com/en/docs/serverless-containers/faq/
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-billing/
- https://www.scaleway.com/en/docs/serverless-functions/reference-content/functions-limitations/
- https://www.scaleway.com/en/docs/serverless-sql-databases/faq/
- https://www.scaleway.com/en/docs/object-storage/faq/
- https://www.scaleway.com/en/docs/account/how-to/create-an-account/
- https://www.scaleway.com/en/docs/account/reference-content/products-availability/
- https://www.scaleway.com/en/about-us/
- https://www.scaleway.com/en/legal-notice/

Oracle
- https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
- https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier.htm
- https://docs.oracle.com/en-us/iaas/Content/General/Concepts/regions.htm
- https://www.oracle.com/cloud/free/ (403)
- https://www.oracle.com/cloud/free/faq/ (403)
- https://www.oracle.com/corporate/ (403)
- https://en.wikipedia.org/wiki/Oracle_Corporation
- https://braindetox.kr/ja/posts/oracle_always_free_backend_2026.html (anecdotal blog)
- https://community.oracle.com/customerconnect/categories/oci-arm-compute (timed out)
- https://community.oracle.com/customerconnect/discussions/tagged/arm (timed out)
- https://community.oracle.com/customerconnect/discussions/tagged/arm-compute/p1 (timed out)

Fly.io
- https://docs.fly.io/about/pricing
- https://docs.fly.io/reference/regions
- https://fly.io/legal/terms-of-service/

Render
- https://render.com/docs/free
- https://render.com/docs/regions
- https://render.com/docs/disks
- https://render.com/pricing.md
- https://render.com/pricing (no body text)
- https://render.com/terms (no body text)
- https://render.com/terms-update (no body text)

Koyeb
- https://www.koyeb.com/pricing
- https://www.koyeb.com/docs/reference/instances
- https://www.koyeb.com/docs/reference/regions
- https://koyeb.com/docs/faqs/pro-plan-trial
- https://www.koyeb.com/about
- https://www.koyeb.com/legal/terms (404), https://www.koyeb.com/terms (404), https://www.koyeb.com/docs/reference/billing (404)
- https://community.koyeb.com/t/hobby-tier-requiring-credit-card/4055 (redirects to login)

Vercel
- https://vercel.com/docs/plans/hobby
- https://vercel.com/docs/limits/fair-use-guidelines
- https://vercel.com/docs/functions/configuring-functions/region
- https://vercel.com/docs/regions
- https://vercel.com/docs/vercel-blob
- https://vercel.com/docs/vercel-blob/usage-and-pricing
- https://vercel.com/docs/cron-jobs/usage-and-pricing
- https://vercel.com/legal/terms

Supabase
- https://supabase.com/pricing
- https://supabase.com/docs/guides/platform/regions
- https://supabase.com/docs/guides/platform/upgrading
- https://supabase.com/terms

Neon, Upstash, Turso
- https://neon.com/pricing
- https://neon.com/docs/introduction/plans
- https://neon.com/docs/introduction/regions
- https://neon.com/terms-of-service
- https://upstash.com/pricing/redis
- https://upstash.com/docs/devops/developer-api/redis/create_database_global
- https://upstash.com/legal/terms (PDF, text not extractable)
- https://turso.tech/pricing
- https://docs.turso.tech/cli/group/create
- https://docs.turso.tech/api-reference/locations/list
- https://docs.turso.tech/cloud-providers (content not matching), https://turso.tech/terms-of-service (404)

Deno Deploy, Netlify
- https://deno.com/deploy/pricing
- https://docs.deno.com/deploy/manual/regions (Classic)
- https://docs.deno.com/deploy/migration_guide/
- https://docs.deno.com/runtime/reference/cli/deploy/
- https://docs.deno.com/deploy/reference/deno_kv/
- https://deno.com/terms
- https://www.netlify.com/pricing/
- https://docs.netlify.com/manage/accounts-and-billing/billing/billing-for-credit-based-plans/credit-based-pricing-plans/
- https://www.netlify.com/legal/terms-of-use/

Paid fallback and rates
- https://www.hetzner.com/cloud/
- https://www.hetzner.com/cloud/cost-optimized/
- https://www.hetzner.com/storage/object-storage/
- https://www.hetzner.com/storage/storage-box/
- https://www.hetzner.com/legal/legal-notice/
- https://docs.hetzner.com/general/others/ipv4-pricing/
- https://docs.hetzner.com/cloud/billing/faq/
- https://docs.hetzner.com/cloud/servers/primary-ips/faq/
- https://docs.hetzner.com/cloud/servers/overview/
- https://www.ovhcloud.com/en/vps/ (USD)
- https://www.ovhcloud.com/en-ie/vps/ (EUR)
- https://www.ovhcloud.com/cs-cz/vps/ (404)
- https://www.ovhcloud.com/en-ie/about-us/
- https://contabo.com/en/vps/
- https://contabo.com/en/about-us/
- https://contabo.com/en/legal-notice/ (404), https://contabo.com/en/imprint/ (404)
- https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing/
- https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/eurofxref-graph-czk.en.html

Web searches (snippets only, not treated as verification): Koyeb plan status, Oracle A1 capacity, Cloudflare rate-limit binding on Free, R2 payment method, Deno Deploy regions, Upstash regions, Netlify credits, Turso locations, Render Starter price, Koyeb domicile, Render legal entity, Scaleway container body size.
