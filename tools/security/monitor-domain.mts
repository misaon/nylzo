import assert from "node:assert/strict";

type Answer = { type: number; data: string };
type Lookup = { AD: boolean; Status: number; Answer?: Answer[] };
type Issuance = { id: string; dns_names: string[]; not_before: string; issuer?: { friendly_name?: string } };
type Registration = { status?: string[]; events?: { eventAction: string; eventDate: string }[] };

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value);
const isStringList = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === "string");
const isOptionalString = (value: unknown): value is string | undefined => value === undefined || typeof value === "string";
const isAnswer = (value: unknown): value is Answer => isRecord(value) && typeof value["type"] === "number" && typeof value["data"] === "string";
const isLookup = (value: unknown): value is Lookup => isRecord(value) && typeof value["AD"] === "boolean" && typeof value["Status"] === "number" &&
  (value["Answer"] === undefined || (Array.isArray(value["Answer"]) && value["Answer"].every(isAnswer)));
const isIssuance = (value: unknown): value is Issuance => isRecord(value) && typeof value["id"] === "string" && isStringList(value["dns_names"]) && typeof value["not_before"] === "string" &&
  (value["issuer"] === undefined || (isRecord(value["issuer"]) && isOptionalString(value["issuer"]["friendly_name"])));
const isEvent = (value: unknown): value is { eventAction: string; eventDate: string } => isRecord(value) && typeof value["eventAction"] === "string" && typeof value["eventDate"] === "string";
const isRegistration = (value: unknown): value is Registration => isRecord(value) && (value["status"] === undefined || isStringList(value["status"])) &&
  (value["events"] === undefined || (Array.isArray(value["events"]) && value["events"].every(isEvent)));

const day = 86400000;
const maxJsonBytes = 1 << 20;
const maxCertificatePages = 50;
const unquote = (data: string) => data.replace(/^"|"$/gu, "").replace(/"\s*"/gu, "");
const parameters = (value: string) => value.split(";").map(part => part.trim()).filter(Boolean).toSorted().join("; ");

export function caaProblems(lookup: Lookup, accountUri?: string) {
  const problems: string[] = [];
  if (!lookup.AD) problems.push("CAA answer is not DNSSEC-validated");
  const records = (lookup.Answer ?? []).filter(answer => answer.type === 257).map(answer => {
    const [, , tag, value] = /^(\d+) (issue|issuewild|iodef) "(.*)"$/u.exec(answer.data.trim()) ?? [];
    return tag !== undefined && value !== undefined ? { tag, value } : { tag: "unparsed", value: answer.data };
  });
  const issue = records.filter(record => record.tag === "issue");
  const wildcard = records.filter(record => record.tag === "issuewild");
  const [onlyIssue, ...otherIssues] = issue;
  if (onlyIssue === undefined || otherIssues.length > 0) problems.push(`expected one CAA issue record, found ${issue.length}`);
  else {
    const value = parameters(onlyIssue.value);
    const required = parameters(["letsencrypt.org", "validationmethods=http-01", ...(accountUri ? [`accounturi=${accountUri}`] : [])].join(";"));
    if (accountUri ? value !== required : !/^accounturi=https:\/\/acme-v02\.api\.letsencrypt\.org\/acme\/acct\/\d+; letsencrypt\.org; validationmethods=http-01$/u.test(value)) {
      problems.push("the CAA issue record does not bind Let's Encrypt to the instance account and HTTP-01");
    }
  }
  if (wildcard.length !== 1 || wildcard[0]?.value.trim() !== ";") problems.push("CAA must forbid every wildcard certificate with one issuewild \";\" record");
  if (records.some(record => !["issue", "issuewild", "iodef"].includes(record.tag))) problems.push("CAA has records this monitor cannot parse");
  return problems;
}

export function mailProblems({ mx, apex, dmarc }: { mx: Lookup; apex: Lookup; dmarc: Lookup }) {
  const problems: string[] = [];
  for (const [name, lookup] of [["MX", mx], ["apex TXT", apex], ["DMARC", dmarc]] as const) if (!lookup.AD) problems.push(`${name} answer is not DNSSEC-validated`);
  const exchangers = (mx.Answer ?? []).filter(answer => answer.type === 15).map(answer => answer.data.trim());
  if (exchangers.length !== 1 || !/^0 \.$/u.test(exchangers[0] ?? "")) problems.push("the domain must publish exactly one null MX record");
  const policies = (apex.Answer ?? []).filter(answer => answer.type === 16).map(answer => unquote(answer.data)).filter(text => text.startsWith("v=spf1"));
  if (policies.length !== 1 || policies[0] !== "v=spf1 -all") problems.push("SPF must be exactly v=spf1 -all");
  const reports = (dmarc.Answer ?? []).filter(answer => answer.type === 16).map(answer => unquote(answer.data)).filter(text => text.startsWith("v=DMARC1"));
  const [report = ""] = reports;
  if (reports.length !== 1 || !/(^|;\s*)p=reject(;|$)/u.test(report) || !/(^|;\s*)sp=reject(;|$)/u.test(report)) problems.push("DMARC must reject the domain and its subdomains");
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
  const body = response.body;
  assert(body, "The monitor API returned an empty body");
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    assert(response.ok, `${new URL(url).host} answered HTTP ${response.status}`);
    const declared = response.headers.get("content-length");
    assert(declared === null || Number(declared) <= maxJsonBytes, "The monitor API response exceeds 1 MiB");
    for await (const chunk of body) {
      length += chunk.byteLength;
      assert(length <= maxJsonBytes, "The monitor API response exceeds 1 MiB");
      chunks.push(chunk);
    }
    return JSON.parse(new TextDecoder().decode(Buffer.concat(chunks, length)));
  } catch (error) {
    await body.cancel().catch(() => {});
    throw error;
  }
}

const registries: Record<string, string> = {
  com: "https://rdap.verisign.com/com/v1/domain/",
  net: "https://rdap.verisign.com/net/v1/domain/",
};

async function readRegistration(domain: string) {
  const base = registries[domain.slice(domain.lastIndexOf(".") + 1)];
  assert(base, "Add the RDAP service of this top-level domain to the monitor");
  const registration = await json(base + domain);
  assert(isRegistration(registration), "The RDAP answer is malformed");
  return registration;
}

async function resolveRecords(name: string, type: string) {
  const lookup = await json(`https://dns.google/resolve?name=${name}&type=${type}&do=true`);
  assert(isLookup(lookup), `The ${type} answer for ${name} is malformed`);
  return lookup;
}

export async function domainProblems(domain: string, options: { accountUri?: string | undefined; reviewedUntil?: string | undefined; now?: number } = {}) {
  assert.match(domain, /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/u);
  const now = options.now ?? Date.now();
  const problems = [
    ...caaProblems(await resolveRecords(domain, "CAA"), options.accountUri),
    ...mailProblems({ mx: await resolveRecords(domain, "MX"), apex: await resolveRecords(domain, "TXT"), dmarc: await resolveRecords(`_dmarc.${domain}`, "TXT") }),
    ...registrationProblems(await readRegistration(domain), now),
  ];
  const since = new Date(Math.max(now - 8 * day, options.reviewedUntil ? new Date(options.reviewedUntil).getTime() : 0));
  for (let after = "", page = 0; page < maxCertificatePages; page++) {
    const batch = await json(`https://api.certspotter.com/v1/issuances?domain=${domain}&include_subdomains=true&expand=dns_names&expand=issuer${after ? `&after=${encodeURIComponent(after)}` : ""}`);
    assert(Array.isArray(batch) && batch.every(isIssuance), "The Certificate Transparency answer is malformed");
    const last = batch.at(-1);
    if (last === undefined) return problems;
    for (const problem of issuanceProblems(batch, domain, since)) problems.push(problem);
    after = last.id;
  }
  throw new Error(`Certificate Transparency check is incomplete after ${maxCertificatePages} pages`);
}

if (import.meta.main) {
  try {
    const problems = await domainProblems(process.env["DOMAIN"] ?? "", { accountUri: process.env["ACME_ACCOUNT"] || undefined, reviewedUntil: process.env["CT_REVIEWED_UNTIL"] || undefined });
    for (const problem of problems) console.error(problem);
    if (problems.length > 0) process.exitCode = 1;
    else console.log("DNS, certificate transparency and registration of the domain match the expected state.");
  } catch (error) {
    console.error(`Domain check failed: ${error instanceof Error ? error.message.split("\n")[0] : "unknown error"}`);
    process.exitCode = 1;
  }
}
