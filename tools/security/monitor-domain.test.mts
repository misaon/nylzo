import assert from "node:assert/strict";
import { mock, test } from "node:test";
import { caaProblems, domainProblems, issuanceProblems, mailProblems, registrationProblems } from "./monitor-domain.mts";

const account = "https://acme-v02.api.letsencrypt.org/acme/acct/3938438202";
const caa = (...data: string[]) => ({ Status: 0, AD: true, Answer: data.map(value => ({ type: 257, data: value })) });
const txt = (...data: string[]) => ({ Status: 0, AD: true, Answer: data.map(value => ({ type: 16, data: value })) });
const now = Date.parse("2026-11-01T12:00:00Z");
const certificate = { id: "7", dns_names: ["nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-31T00:00:00Z" };
const domainAnswers: Record<string, unknown> = {
  "https://dns.google/resolve?name=nylzo.com&type=CAA&do=true": caa(`0 issue "letsencrypt.org; validationmethods=http-01; accounturi=${account}"`, '0 issuewild ";"'),
  "https://dns.google/resolve?name=nylzo.com&type=MX&do=true": { Status: 0, AD: true, Answer: [{ type: 15, data: "0 ." }] },
  "https://dns.google/resolve?name=nylzo.com&type=TXT&do=true": txt('"v=spf1 -all"'),
  "https://dns.google/resolve?name=_dmarc.nylzo.com&type=TXT&do=true": txt('"v=DMARC1; p=reject; sp=reject"'),
  "https://rdap.verisign.com/com/v1/domain/nylzo.com": { status: ["client transfer prohibited", "client update prohibited", "client delete prohibited"], events: [{ eventAction: "expiration", eventDate: "2030-10-25T12:25:04Z" }] },
};

test("CAA must name only Let's Encrypt for the instance account by HTTP-01 and forbid wildcards", () => {
  const audited = caa(...["comodoca.com", "digicert.com; cansignhttpexchanges=yes", "letsencrypt.org", "pki.goog; cansignhttpexchanges=yes", "ssl.com"].flatMap(issuer => [`0 issue "${issuer}"`, `0 issuewild "${issuer}"`]), '0 issuewild ";"');
  assert.notDeepEqual(caaProblems(audited, account), []);
  const expected = caa(`0 issue "letsencrypt.org; validationmethods=http-01; accounturi=${account}"`, '0 issuewild ";"');
  assert.deepEqual(caaProblems(expected, account), []);
  assert.deepEqual(caaProblems(expected), []);
  for (const changed of [
    caa('0 issue "letsencrypt.org; validationmethods=http-01"', '0 issuewild ";"'),
    caa(`0 issue "letsencrypt.org; validationmethods=http-01; accounturi=${account}"`),
    caa(`0 issue "letsencrypt.org; validationmethods=http-01; accounturi=${account}"`, '0 issuewild ";"', '0 issue "pki.goog"'),
    caa(`0 issue "letsencrypt.org; validationmethods=dns-01; accounturi=${account}"`, '0 issuewild ";"'),
    caa(`0 issue "letsencrypt.org; validationmethods=http-01; accounturi=${account}9"`, '0 issuewild ";"'),
    { ...expected, AD: false },
  ]) assert.notDeepEqual(caaProblems(changed, account), [], JSON.stringify(changed));
});

test("the domain publishes a null MX, a hard-fail SPF and a rejecting DMARC policy", () => {
  const mx = { Status: 0, AD: true, Answer: [{ type: 15, data: "0 ." }] };
  assert.deepEqual(mailProblems(mx, txt('"v=spf1 -all"', '"google-site-verification=x"'), txt('"v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s"')), []);
  assert.equal(mailProblems({ Status: 0, AD: true }, { Status: 0, AD: true }, { Status: 3, AD: true }).length, 3);
  assert.notDeepEqual(mailProblems(mx, txt('"v=spf1 ~all"'), txt('"v=DMARC1; p=none"')), []);
});

test("certificate transparency accepts only Let's Encrypt certificates for the instance names", () => {
  const audited = [
    { id: "18019271813", dns_names: ["*.nylzo.com", "nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-27T18:15:03Z" },
    { id: "18019570941", dns_names: ["nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-27T18:37:48Z" },
    { id: "18054037351", dns_names: ["*.nylzo.com", "nylzo.com"], issuer: { friendly_name: "Sectigo" }, not_before: "2026-10-29T00:00:00Z" },
  ];
  const since = new Date(now - 8 * 86400000);
  assert.deepEqual(issuanceProblems(audited, "nylzo.com", since), ["certificate 18019271813 names *.nylzo.com", "certificate 18054037351 comes from Sectigo", "certificate 18054037351 names *.nylzo.com"]);
  assert.deepEqual(issuanceProblems(audited, "nylzo.com", new Date("2026-10-30T00:00:00Z")), []);
  assert.deepEqual(issuanceProblems([{ id: "1", dns_names: ["www.nylzo.com", "nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-31T00:00:00Z" }], "nylzo.com", since), []);
});

test("the registration carries the registrar lock and does not expire within 180 days", () => {
  const audited = { status: ["active"], events: [{ eventAction: "expiration", eventDate: "2027-10-25T12:25:04Z" }] };
  assert.equal(registrationProblems(audited, now).length, 3);
  const locked = { status: ["client transfer prohibited", "client update prohibited", "client delete prohibited"], events: audited.events };
  assert.deepEqual(registrationProblems(locked, now), []);
  assert.deepEqual(registrationProblems({ ...locked, events: [{ eventAction: "expiration", eventDate: "2027-03-01T00:00:00Z" }] }, now), ["the registration expires within 180 days"]);
});

test("the domain check reads DNS through a validating resolver, RDAP and every Cert Spotter page", async () => {
  const seen: string[] = [];
  const answers: Record<string, unknown> = {
    ...domainAnswers,
    "https://api.certspotter.com/v1/issuances?domain=nylzo.com&include_subdomains=true&expand=dns_names&expand=issuer": [{ id: "7", dns_names: ["nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-31T00:00:00Z" }],
    "https://api.certspotter.com/v1/issuances?domain=nylzo.com&include_subdomains=true&expand=dns_names&expand=issuer&after=7": [{ id: "8", dns_names: ["*.nylzo.com"], issuer: { friendly_name: "Let's Encrypt" }, not_before: "2026-10-31T01:00:00Z" }],
    "https://api.certspotter.com/v1/issuances?domain=nylzo.com&include_subdomains=true&expand=dns_names&expand=issuer&after=8": [],
  };
  const fetch = mock.method(globalThis, "fetch", async (url: string) => {
    seen.push(url);
    return url in answers ? new Response(JSON.stringify(answers[url]), { status: 200 }) : new Response("", { status: 404 });
  });
  try {
    assert.deepEqual(await domainProblems("nylzo.com", { accountUri: account, now }), ["certificate 8 names *.nylzo.com"]);
    assert.equal(seen.length, Object.keys(answers).length);
    await assert.rejects(domainProblems("nylzo.com/../x", { now }));
  } finally { fetch.mock.restore(); }
});

test("certificate monitoring fails closed unless pagination reaches an empty page", async () => {
  for (const complete of [false, true]) {
    let pages = 0;
    const fetch = mock.method(globalThis, "fetch", async (url: string) => {
      if (url in domainAnswers) return Response.json(domainAnswers[url]);
      pages++;
      assert.equal(new URL(url).searchParams.get("after"), pages === 1 ? null : String(pages - 1));
      return Response.json(complete && pages === 50 ? [] : [{ ...certificate, id: String(pages) }]);
    });
    try {
      if (complete) assert.deepEqual(await domainProblems("nylzo.com", { accountUri: account, now }), []);
      else await assert.rejects(domainProblems("nylzo.com", { accountUri: account, now }), /incomplete after 50 pages/u);
      assert.equal(pages, 50);
    } finally { fetch.mock.restore(); }
  }
});

test("monitor API bodies are bounded before parsing and cancelled on overflow", async () => {
  for (const declared of [false, true]) {
    let reads = 0;
    let cancelled = false;
    const fetch = mock.method(globalThis, "fetch", async () => new Response(new ReadableStream<Uint8Array>({
      pull(controller) {
        reads++;
        if (reads <= 2) controller.enqueue(new Uint8Array(reads === 1 ? 1 << 20 : 1));
        else controller.close();
      },
      cancel() { cancelled = true; },
    }, { highWaterMark: 0 }), { headers: declared ? { "content-length": String((1 << 20) + 1) } : {} }));
    try {
      await assert.rejects(domainProblems("nylzo.com", { accountUri: account, now }), /response exceeds 1 MiB/u);
      assert.equal(reads, declared ? 0 : 2);
      assert.equal(cancelled, true);
    } finally { fetch.mock.restore(); }
  }
});

test("a monitor API response at the byte limit is accepted without following redirects", async () => {
  const fetch = mock.method(globalThis, "fetch", async (url: string, options?: RequestInit) => {
    assert.equal(options?.redirect, "error");
    const body = JSON.stringify(url in domainAnswers ? domainAnswers[url] : []);
    return new Response(body.padEnd(1 << 20), { headers: { "content-length": String(1 << 20) } });
  });
  try {
    assert.deepEqual(await domainProblems("nylzo.com", { accountUri: account, now }), []);
  } finally { fetch.mock.restore(); }
});
