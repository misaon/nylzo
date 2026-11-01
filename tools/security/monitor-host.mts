import assert from "node:assert/strict";
import { X509Certificate } from "node:crypto";
import { request, type RequestOptions } from "node:https";
import { isIP, type LookupFunction } from "node:net";
import { privateFile } from "./private-file.mts";

const components = ["hostPolicy", "services", "diskReserve", "applicationUpdates", "osUpdates", "clock", "certificate"];

export function hostConnection(address?: string): Pick<RequestOptions, "lookup"> {
  if (!address) return {};
  const family = isIP(address);
  assert(family, "The optional host connection pin must be an IP address");
  const lookup: LookupFunction = (_hostname, options, callback) => options.all
    ? callback(null, [{ address, family }])
    : callback(null, address, family);
  return { lookup };
}

export function validateHostStatus(input: any, now = Date.now()) {
  assert.equal(input?.version, 1, "Unknown host-status format");
  assert(Number.isSafeInteger(input.observedAt), "Missing observation time");
  const age = now / 1000 - input.observedAt;
  assert(age >= -60 && age <= 360, "Host-status observation is stale or from the future");
  for (const component of components) assert.equal(input.checks?.[component], true, `Host readiness failed: ${component}`);
}

export async function readHostStatus(url: URL, identity: { cert: string; key: string; ca?: string }, connection: Pick<RequestOptions, "lookup"> = {}) {
  assert.equal(url.protocol, "https:");
  assert.equal(url.username + url.password + url.search + url.hash, "");
  assert.equal(url.pathname, "/healthz");
  const certificate = new X509Certificate(identity.cert);
  assert(certificate.validToDate.getTime() - Date.now() > 30 * 86400000, "Monitor identity needs rotation within 30 days");
  return new Promise<unknown>((resolve, reject) => {
    let deadline: ReturnType<typeof setTimeout> | undefined;
    const fail = (error: Error) => { clearTimeout(deadline); reject(error); };
    const outgoing = request(url, { ...identity, ...connection, method: "GET", agent: false, minVersion: "TLSv1.3", rejectUnauthorized: true, maxHeaderSize: 8192 }, response => {
      if (response.statusCode !== 200 || !/^application\/json(?:;|$)/iu.test(response.headers["content-type"] ?? "")) {
        response.destroy(); fail(new Error("Unexpected host-status response")); return;
      }
      const chunks: Buffer[] = [];
      let length = 0;
      response.on("data", (chunk: Buffer) => {
        length += chunk.length;
        if (length > 8192) { fail(new Error("Host-status response exceeds its bound")); response.destroy(); return; }
        chunks.push(chunk);
      });
      response.on("error", fail);
      response.on("aborted", () => fail(new Error("Incomplete host-status response")));
      response.on("end", () => {
        clearTimeout(deadline);
        try { resolve(JSON.parse(Buffer.concat(chunks).toString("utf8"))); } catch { fail(new Error("Invalid host-status JSON")); }
      });
    });
    deadline = setTimeout(() => outgoing.destroy(new Error("Host-status request timed out")), 20_000);
    outgoing.on("error", fail);
    outgoing.end();
  });
}

export async function requireClientAuthentication(url: URL, connection: Pick<RequestOptions, "lookup"> = {}, ca?: string) {
  assert.equal(url.protocol, "https:");
  assert.equal(url.username + url.password + url.search + url.hash, "");
  assert.equal(url.pathname, "/healthz");
  return new Promise<void>((resolve, reject) => {
    const outgoing = request(url, { ...connection, ca, method: "GET", agent: false, minVersion: "TLSv1.3", rejectUnauthorized: true }, response => {
      response.destroy(); clearTimeout(deadline);
      reject(new Error("The host-status listener accepted an unauthenticated TLS client"));
    });
    const deadline = setTimeout(() => outgoing.destroy(new Error("TLS denial probe timed out")), 20_000);
    outgoing.on("error", (error: NodeJS.ErrnoException) => {
      clearTimeout(deadline);
      if (error.code === "ERR_SSL_TLSV13_ALERT_CERTIFICATE_REQUIRED") resolve();
      else reject(new Error("The expected TLS client-certificate denial was not observed"));
    });
    outgoing.end();
  });
}

if (import.meta.main) {
  try {
    const cert = process.env.MONITOR_CERT ?? await privateFile(process.env.MONITOR_CERT_FILE!);
    const key = process.env.MONITOR_KEY ?? await privateFile(process.env.MONITOR_KEY_FILE!);
    const result = await readHostStatus(new URL(process.env.URL!), { cert, key }, hostConnection(process.env.MONITOR_ADDRESS));
    validateHostStatus(result);
    await requireClientAuthentication(new URL(process.env.URL!), hostConnection(process.env.MONITOR_ADDRESS));
    console.log("Authenticated host policy, services, disk reserve, and update checks are healthy.");
  } catch {
    // TLS and HTTP errors may include endpoint details. Never print credentials or response bodies.
    console.error("Authenticated host readiness check failed. Check host state, observation freshness, and monitor identity expiry.");
    process.exitCode = 1;
  }
}
