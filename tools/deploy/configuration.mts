import assert from "node:assert/strict";
import { isIP } from "node:net";
import { createPublicKey } from "node:crypto";

export type Site = {
  name: string;
  domain: string;
  location: string;
  serverType: string;
  architecture: "amd64" | "arm64";
  administratorNetworks: string[];
  administratorPublicKey: string;
  reportPublicKey?: string | undefined;
  operator: string;
  contact: string;
  hosting: string;
  dailyCreates: number;
  initialRelease: string;
  automaticUpdates: boolean;
  dnsZone?: string | undefined;
  cloudflareAccountId?: string | undefined;
  publicAccess?: boolean | undefined;
  heartbeatUrl?: string | undefined;
  tang?: { servers: { url: string; thumbprint: string }[]; threshold: number } | undefined;
  acmeAccountUri?: string | undefined;
  profile?: "strict" | undefined;
};

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function assertText(value: unknown, pattern: RegExp, message?: string): asserts value is string {
  if (message === undefined) {
    assert(typeof value === "string");
    assert.match(value, pattern);
  } else {
    assert(typeof value === "string", message);
    assert.match(value, pattern, message);
  }
}

export function configuration(input: unknown): Site {
  assertSite(input);
  return input;
}

function assertSite(input: unknown): asserts input is Site {
  const allowed = ["name", "domain", "location", "serverType", "architecture", "administratorNetworks", "administratorPublicKey", "reportPublicKey", "operator", "contact", "hosting", "dailyCreates", "initialRelease", "automaticUpdates", "dnsZone", "cloudflareAccountId", "publicAccess", "heartbeatUrl", "tang", "acmeAccountUri", "profile"];
  assert(isRecord(input));
  assert(Object.keys(input).every(key => allowed.includes(key)), "Unknown deployment option");
  const { name, domain, location, serverType, architecture, administratorNetworks, administratorPublicKey, reportPublicKey, operator, contact, hosting, dailyCreates, initialRelease, automaticUpdates, dnsZone, cloudflareAccountId, publicAccess, heartbeatUrl, tang, acmeAccountUri, profile } = input;
  assertText(name, /^[a-z][a-z0-9-]{0,30}$/u);
  assertText(domain, /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/u);
  assertText(location, /^(nbg1|fsn1|hel1)$/u);
  assertText(serverType, /^(cx|cpx|cax|ccx)[0-9]{2,3}$/u);
  assert(architecture === "amd64" || architecture === "arm64");
  assert.equal(architecture === "arm64", serverType.startsWith("cax"), "Server type and architecture differ");
  assert(Array.isArray(administratorNetworks) && administratorNetworks.length > 0);
  const networks: unknown[] = administratorNetworks;
  for (const network of networks) {
    assert(typeof network === "string");
    const [address = "", prefix, extra] = network.split("/");
    const family = isIP(address);
    assert(family && extra === undefined && prefix !== undefined && /^\d+$/u.test(prefix) && Number(prefix) >= (family === 4 ? 24 : 64) && Number(prefix) <= (family === 4 ? 32 : 128), "Administrator CIDRs must be /24 or narrower for IPv4 and /64 or narrower for IPv6");
  }
  administratorKeyIsHardware(administratorPublicKey);
  for (const metadata of [operator, contact, hosting]) {
    assert(typeof metadata === "string");
    assert(metadata.length <= 300 && !/[\r\n\0]/u.test(metadata), "Invalid public instance metadata");
  }
  assertText(contact, /^(?:[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+|https:\/\/[\w.~:/?@!&+,;=%-]+)?$/u, "The contact is one email address or https:// address");
  assert(typeof dailyCreates === "number" && Number.isSafeInteger(dailyCreates) && dailyCreates > 0 && dailyCreates <= 500, "This small-server profile permits at most 500 daily creations");
  assertText(initialRelease, /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u, "Set initialRelease to the tag of a Nylzo release that ships the Caddy build");
  // Releases before 0.14.0 refuse to start without the report key, so a site that boots one must carry it.
  const [major = 0, minor = 0] = initialRelease.slice(1).split(".").map(Number);
  if (reportPublicKey === undefined) {
    assert(major > 0 || minor >= 14, "A release before v0.14.0 needs reportPublicKey; set initialRelease to v0.14.0 or later");
  } else {
    assertText(reportPublicKey, /^[A-Za-z0-9_-]{1622}$/u);
    assert.equal(Buffer.from(reportPublicKey, "base64url").length, 1216);
  }
  assert(typeof automaticUpdates === "boolean");
  if (heartbeatUrl !== undefined) assertText(heartbeatUrl, /^https:\/\/[a-z0-9.-]{1,253}(?::[0-9]{1,5})?(?:\/[A-Za-z0-9._~-]+){1,8}$/u, "The heartbeat address must be one HTTPS URL without a query or fragment");
  if (tang !== undefined) {
    assert(isRecord(tang));
    const { servers, threshold, ...unknownOptions } = tang;
    assert.deepEqual(unknownOptions, {}, "Unknown Tang option");
    assert(Array.isArray(servers) && servers.length >= 2 && servers.length <= 5, "Configure two to five Tang servers at different providers");
    const listed: unknown[] = servers;
    const urls = new Set<string>();
    for (const server of listed) {
      assert(isRecord(server));
      assert.deepEqual(Object.keys(server).toSorted(), ["thumbprint", "url"]);
      const { url, thumbprint } = server;
      assertText(url, /^https?:\/\/[a-z0-9.-]{1,253}(?::[0-9]{1,5})?$/u, "A Tang URL is a scheme, host and optional port");
      assertText(thumbprint, /^[A-Za-z0-9_-]{43}$/u, "A Tang thumbprint is the base64url SHA-256 JWK thumbprint of its signing key");
      urls.add(url);
    }
    assert.equal(urls.size, listed.length, "Tang servers must differ");
    assert(typeof threshold === "number" && Number.isSafeInteger(threshold) && threshold >= 1 && threshold <= listed.length, "The Tang threshold must be between 1 and the number of servers");
  }
  if (profile !== undefined) {
    assert.equal(profile, "strict", "The only deployment profile is strict");
    assert(tang !== undefined, "The strict profile requires the encrypted data volume; configure tang");
  }
  if (acmeAccountUri !== undefined) assertText(acmeAccountUri, /^https:\/\/acme-v02\.api\.letsencrypt\.org\/acme\/acct\/[1-9][0-9]{0,19}$/u, "Set acmeAccountUri to the Let's Encrypt account URL from the server's Caddy storage");
  if (publicAccess !== undefined) assert(typeof publicAccess === "boolean");
  if (dnsZone !== undefined) assert(typeof dnsZone === "string" && (domain === dnsZone || domain.endsWith(`.${dnsZone}`)), "DNS zone does not contain the instance domain");
  if (cloudflareAccountId !== undefined && cloudflareAccountId !== "") assertText(cloudflareAccountId, /^[a-f0-9]{32}$/u);
}

export function patchCandidate({ current, candidate }: { current: string; candidate: string }) {
  const stable = /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u;
  const previous = stable.exec(current);
  const next = stable.exec(candidate);
  return !!previous && !!next && [...previous.slice(1), ...next.slice(1)].every(value => Number.isSafeInteger(Number(value)) && Number(value) <= 999999999) && previous[1] === next[1] && previous[2] === next[2] && Number(next[3]) > Number(previous[3]);
}

const application = (value: Buffer | undefined) => assert.match(value?.toString("latin1") ?? "", /^ssh:[!-~]{0,64}$/u, "Invalid security key application");

export function administratorKeyIsHardware(line: unknown) {
  assert(typeof line === "string");
  const match = /^(ssh-ed25519|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) ([A-Za-z0-9+/]+={0,2})(?: [^\r\n]+)?$/u.exec(line);
  assert(match, "Use an Ed25519 SSH key, or better a hardware-backed sk-ssh-ed25519@openssh.com or sk-ecdsa-sha2-nistp256@openssh.com key");
  const [, type = "", encoded = ""] = match;
  const blob = Buffer.from(encoded, "base64");
  assert.equal(blob.toString("base64"), encoded, "Invalid SSH key encoding");
  const fields: Buffer[] = [];
  for (let offset = 0; offset < blob.length;) {
    assert(offset + 4 <= blob.length, "Invalid SSH key encoding");
    const length = blob.readUInt32BE(offset);
    offset += 4;
    assert(offset + length <= blob.length, "Invalid SSH key encoding");
    fields.push(blob.subarray(offset, offset + length));
    offset += length;
  }
  assert.equal(fields[0]?.toString("latin1"), type, "Invalid SSH key encoding");
  if (type === "sk-ecdsa-sha2-nistp256@openssh.com") {
    assert.equal(fields.length, 4, "Invalid SSH key encoding");
    assert.equal(fields[1]?.toString("latin1"), "nistp256", "Invalid ECDSA SSH key encoding");
    const point = fields[2];
    assert(point !== undefined && point.length === 65 && point[0] === 4, "Invalid ECDSA SSH key encoding");
    assert.doesNotThrow(() => createPublicKey({ key: { kty: "EC", crv: "P-256", x: point.subarray(1, 33).toString("base64url"), y: point.subarray(33).toString("base64url") }, format: "jwk" }), "Invalid ECDSA SSH key point");
    application(fields[3]);
    return true;
  }
  const hardware = type === "sk-ssh-ed25519@openssh.com";
  assert.equal(fields.length, hardware ? 3 : 2, "Invalid SSH key encoding");
  assert.equal(fields[1]?.length, 32, "Invalid Ed25519 SSH key encoding");
  if (hardware) application(fields[2]);
  return hardware;
}
