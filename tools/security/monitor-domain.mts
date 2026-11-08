import assert from "node:assert/strict";

type Answer = { type: number; data: string };
type Lookup = { AD: boolean; Status: number; Answer?: Answer[] };
type Issuance = { id: string; dns_names: string[]; not_before: string; issuer?: { friendly_name?: string } };
type Registration = { status?: string[]; events?: { eventAction: string; eventDate: string }[] };

const day = 86400000;
const maxJsonBytes = 1 << 20;
const maxCertificatePages = 50;
const unquote = (data: string) => data.replace(/^"|"$/gu, "").replace(/"\s*"/gu, "");
const parameters = (value: string) => value.split(";").map(part => part.trim()).filter(Boolean).sort().join("; ");

export function caaProblems(lookup: Lookup, accountUri?: string) {
  const problems: string[] = [];
  if (!lookup.AD) problems.push("CAA answer is not DNSSEC-validated");
  const records = (lookup.Answer ?? []).filter(answer => answer.type === 257).map(answer => {
    const match = /^(\d+) (issue|issuewild|iodef) "(.*)"$/u.exec(answer.data.trim());
    return match ? { tag: match[2]!, value: match[3]! } : { tag: "unparsed", value: answer.data };
  });
  const issue = records.filter(record => record.tag === "issue");
  const wildcard = records.filter(record => record.tag === "issuewild");
  if (issue.length !== 1) problems.push(`expected one CAA issue record, found ${issue.length}`);
  else {
    const value = parameters(issue[0]!.value);
    const required = parameters(["letsencrypt.org", "validationmethods=http-01", ...(accountUri ? [`accounturi=${accountUri}`] : [])].join(";"));
    if (accountUri ? value !== required : !/^accounturi=https:\/\/acme-v02\.api\.letsencrypt\.org\/acme\/acct\/\d+; letsencrypt\.org; validationmethods=http-01$/u.test(value)) {
      problems.push("the CAA issue record does not bind Let's Encrypt to the instance account and HTTP-01");
    }
  }
  if (wildcard.length !== 1 || wildcard[0]!.value.trim() !== ";") problems.push("CAA must forbid every wildcard certificate with one issuewild \";\" record");
  if (records.some(record => !["issue", "issuewild", "iodef"].includes(record.tag))) problems.push("CAA has records this monitor cannot parse");
  return problems;
}

export function mailProblems(mx: Lookup, apex: Lookup, dmarc: Lookup) {
  const problems: string[] = [];
  for (const [name, lookup] of [["MX", mx], ["apex TXT", apex], ["DMARC", dmarc]] as const) if (!lookup.AD) problems.push(`${name} answer is not DNSSEC-validated`);
  const exchangers = (mx.Answer ?? []).filter(answer => answer.type === 15).map(answer => answer.data.trim());
  if (exchangers.length !== 1 || !/^0 \.$/u.test(exchangers[0]!)) problems.push("the domain must publish exactly one null MX record");
  const policies = (apex.Answer ?? []).filter(answer => answer.type === 16).map(answer => unquote(answer.data)).filter(text => text.startsWith("v=spf1"));
  if (policies.length !== 1 || policies[0] !== "v=spf1 -all") problems.push("SPF must be exactly v=spf1 -all");
  const reports = (dmarc.Answer ?? []).filter(answer => answer.type === 16).map(answer => unquote(answer.data)).filter(text => text.startsWith("v=DMARC1"));
  if (reports.length !== 1 || !/(^|;\s*)p=reject(;|$)/u.test(reports[0]!) || !/(^|;\s*)sp=reject(;|$)/u.test(reports[0]!)) problems.push("DMARC must reject the domain and its subdomains");
  return problems;
}

export function issuanceProblems(issuances: Issuance[], domain: string, since: Date) {
  const allowed = new Set([domain, `www.${domain}`]);
  return issuances.filter(issuance => new Date(issuance.not_before) >= since).flatMap(issuance => {
    const problems: string[] = [];
    if (!/^Let's Encrypt\b/u.test(issuance.issuer?.friendly_name ?? "")) problems.push(`certificate ${issuance.id} comes from ${issuance.issuer?.friendly_name ?? "an unknown issuer"}`);
    for (const name of issuance.dns_names) if (!allowed.has(name)) problems.push(`certificate ${issuance.id} names ${name}`);
    return problems;
  });
}

export function registrationProblems(registration: Registration, now = Date.now()) {
  const problems: string[] = [];
  const status = new Set(registration.status ?? []);
  for (const lock of ["client transfer prohibited", "client update prohibited", "client delete prohibited"]) if (!status.has(lock)) problems.push(`the registrar lock lacks ${lock}`);
  const expiration = registration.events?.find(event => event.eventAction === "expiration");
  if (!expiration || new Date(expiration.eventDate).getTime() - now < 180 * day) problems.push("the registration expires within 180 days");
  return problems;
}

async function json(url: string): Promise<unknown> {
  const response = await fetch(url, { headers: { accept: "application/json" }, redirect: "error", signal: AbortSignal.timeout(30000) });
  const reader = response.body?.getReader();
  assert(reader, "The monitor API returned an empty body");
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    assert(response.ok, `${new URL(url).host} answered HTTP ${response.status}`);
    const declared = response.headers.get("content-length");
    assert(declared === null || Number(declared) <= maxJsonBytes, "The monitor API response exceeds 1 MiB");
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      assert(length <= maxJsonBytes, "The monitor API response exceeds 1 MiB");
      chunks.push(value);
    }
    return JSON.parse(new TextDecoder().decode(Buffer.concat(chunks, length)));
  } catch (error) {
    await reader.cancel().catch(() => {});
    throw error;
  } finally {
    reader.releaseLock();
  }
}

const registries: Record<string, string> = {
  com: "https://rdap.verisign.com/com/v1/domain/",
  net: "https://rdap.verisign.com/net/v1/domain/",
};

function registration(domain: string) {
  const base = registries[domain.slice(domain.lastIndexOf(".") + 1)];
  assert(base, "Add the RDAP service of this top-level domain to the monitor");
  return json(base + domain) as Promise<Registration>;
}

export async function domainProblems(domain: string, options: { accountUri?: string; reviewedUntil?: string; now?: number } = {}) {
  assert.match(domain, /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/u);
  const now = options.now ?? Date.now();
  const resolve = (name: string, type: string) => json(`https://dns.google/resolve?name=${name}&type=${type}&do=true`) as Promise<Lookup>;
  const problems = [
    ...caaProblems(await resolve(domain, "CAA"), options.accountUri),
    ...mailProblems(await resolve(domain, "MX"), await resolve(domain, "TXT"), await resolve(`_dmarc.${domain}`, "TXT")),
    ...registrationProblems(await registration(domain), now),
  ];
  const since = new Date(Math.max(now - 8 * day, options.reviewedUntil ? new Date(options.reviewedUntil).getTime() : 0));
  for (let after = "", page = 0; page < maxCertificatePages; page++) {
    const batch = await json(`https://api.certspotter.com/v1/issuances?domain=${domain}&include_subdomains=true&expand=dns_names&expand=issuer${after ? `&after=${encodeURIComponent(after)}` : ""}`) as Issuance[];
    if (batch.length === 0) return problems;
    for (const problem of issuanceProblems(batch, domain, since)) problems.push(problem);
    after = batch.at(-1)!.id;
  }
  throw new Error(`Certificate Transparency check is incomplete after ${maxCertificatePages} pages`);
}

if (import.meta.main) {
  try {
    const problems = await domainProblems(process.env.DOMAIN ?? "", { accountUri: process.env.ACME_ACCOUNT || undefined, reviewedUntil: process.env.CT_REVIEWED_UNTIL || undefined });
    for (const problem of problems) console.error(problem);
    if (problems.length > 0) process.exitCode = 1;
    else console.log("DNS, certificate transparency and registration of the domain match the expected state.");
  } catch (error) {
    console.error(`Domain check failed: ${error instanceof Error ? error.message.split("\n")[0] : "unknown error"}`);
    process.exitCode = 1;
  }
}
