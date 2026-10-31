import assert from "node:assert/strict";
import { isIP } from "node:net";
import { X509Certificate } from "node:crypto";

export function configuration(input: any) {
  const allowed = ["name", "domain", "location", "serverType", "architecture", "administratorNetworks", "administratorPublicKey", "reportPublicKey", "operator", "contact", "hosting", "dailyCreates", "initialRelease", "automaticUpdates", "dnsZone", "cloudflareAccountId", "publicAccess", "monitorClientCertificate"];
  assert(input && typeof input === "object" && !Array.isArray(input));
  assert(Object.keys(input).every(key => allowed.includes(key)), "Unknown deployment option");
  assert.match(input.name, /^[a-z][a-z0-9-]{0,30}$/u);
  assert.match(input.domain, /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/u);
  assert.match(input.location, /^(nbg1|fsn1|hel1)$/u);
  assert.match(input.serverType, /^(cx|cpx|cax|ccx)[0-9]{2,3}$/u);
  assert(["amd64", "arm64"].includes(input.architecture));
  assert.equal(input.architecture === "arm64", input.serverType.startsWith("cax"), "Server type and architecture differ");
  assert(Array.isArray(input.administratorNetworks) && input.administratorNetworks.length > 0);
  for (const network of input.administratorNetworks) {
    assert.equal(typeof network, "string");
    const [address, prefix, extra] = network.split("/");
    const family = isIP(address!);
    assert(family && extra === undefined && /^\d+$/u.test(prefix!) && Number(prefix) >= (family === 4 ? 24 : 64) && Number(prefix) <= (family === 4 ? 32 : 128), "Administrator CIDRs must be /24 or narrower for IPv4 and /64 or narrower for IPv6");
  }
  assert.match(input.administratorPublicKey, /^ssh-ed25519 [A-Za-z0-9+/]{68}(?: [^\r\n]+)?$/u);
  const sshKey = Buffer.from(input.administratorPublicKey.split(" ")[1], "base64");
  assert.equal(sshKey.length, 51);
  assert.equal(sshKey.subarray(0, 19).toString("hex"), "0000000b7373682d6564323535313900000020", "Invalid Ed25519 SSH key encoding");
  assert.match(input.reportPublicKey, /^[A-Za-z0-9_-]{1622}$/u);
  assert.equal(Buffer.from(input.reportPublicKey, "base64url").length, 1216);
  for (const field of ["operator", "contact", "hosting"]) {
    assert.equal(typeof input[field], "string");
    assert(input[field].length <= 300 && !/[\r\n\0]/u.test(input[field]), "Invalid public instance metadata");
  }
  assert(Number.isSafeInteger(input.dailyCreates) && input.dailyCreates > 0 && input.dailyCreates <= 500, "This small-server profile permits at most 500 daily creations");
  assert.match(input.initialRelease, /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u, "Set initialRelease to the tag of a Nylzo release that ships the Caddy build");
  assert.equal(typeof input.automaticUpdates, "boolean");
  if (input.monitorClientCertificate !== undefined) {
    assert(input.automaticUpdates, "Host monitoring requires automatic update checks");
    assert.equal(typeof input.monitorClientCertificate, "string");
    assert(input.monitorClientCertificate.length <= 4096);
    assert(!input.monitorClientCertificate.includes("PRIVATE KEY"), "Supply only the public monitor certificate");
    const certificate = new X509Certificate(input.monitorClientCertificate);
    assert(certificate.toString().trim() === input.monitorClientCertificate.trim(), "Supply one public certificate, never a private key or a certificate bundle");
    assert(!certificate.ca && certificate.verify(certificate.publicKey), "Use a self-signed monitor leaf identity");
    assert(certificate.keyUsage?.includes("1.3.6.1.5.5.7.3.2"), "The monitor identity must permit TLS client authentication");
    const key = certificate.publicKey;
    assert(key.asymmetricKeyType === "ed25519" ||
      (key.asymmetricKeyType === "ec" && ["prime256v1", "secp384r1", "secp521r1"].includes(key.asymmetricKeyDetails?.namedCurve ?? "")) ||
      (["rsa", "rsa-pss"].includes(key.asymmetricKeyType ?? "") && (key.asymmetricKeyDetails?.modulusLength ?? 0) >= 3072), "Use a monitor key with at least 128-bit classical security");
    assert(certificate.validToDate.getTime() - certificate.validFromDate.getTime() <= 397 * 86400000, "Monitor certificates must expire within 397 days");
    assert(certificate.validFromDate.getTime() <= Date.now() && certificate.validToDate.getTime() > Date.now() + 30 * 86400000, "Rotate the monitor identity at least 30 days before expiry");
  }
  if (input.publicAccess !== undefined) assert.equal(typeof input.publicAccess, "boolean");
  if (input.dnsZone !== undefined) assert(input.domain === input.dnsZone || input.domain.endsWith(`.${input.dnsZone}`), "DNS zone does not contain the instance domain");
  if (input.cloudflareAccountId) assert.match(input.cloudflareAccountId, /^[a-f0-9]{32}$/u);
  return input;
}

export function patchCandidate(current: string, candidate: string) {
  const stable = /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u;
  const previous = stable.exec(current);
  const next = stable.exec(candidate);
  return !!previous && !!next && [...previous.slice(1), ...next.slice(1)].every(value => Number.isSafeInteger(Number(value)) && Number(value) <= 999999999) && previous[1] === next[1] && previous[2] === next[2] && Number(next[3]) > Number(previous[3]);
}
