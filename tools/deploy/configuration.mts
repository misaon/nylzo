import assert from "node:assert/strict";
import { isIP } from "node:net";

export function configuration(input: any) {
  const allowed = ["name", "domain", "location", "serverType", "architecture", "administratorNetworks", "administratorPublicKey", "reportPublicKey", "operator", "contact", "hosting", "dailyCreates", "initialRelease", "automaticUpdates", "dnsZone", "cloudflareAccountId"];
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
  assert.match(input.initialRelease, /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u);
  assert.equal(typeof input.automaticUpdates, "boolean");
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
