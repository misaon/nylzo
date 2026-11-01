import assert from "node:assert/strict";
import test from "node:test";
import { hostConnection, validateHostStatus } from "./monitor-host.mts";

test("host monitoring rejects missing, unhealthy, stale, future, and incorrectly typed observations", () => {
  const now = Date.now();
  const healthy = { version: 1, observedAt: Math.floor(now / 1000), checks: { hostPolicy: true, services: true, diskReserve: true, applicationUpdates: true, osUpdates: true, clock: true, certificate: true } };
  validateHostStatus(healthy, now);
  for (const input of [null, {}, { ...healthy, version: 2 }, { ...healthy, observedAt: "now" }, { ...healthy, observedAt: healthy.observedAt - 361 }, { ...healthy, observedAt: healthy.observedAt + 62 }]) assert.throws(() => validateHostStatus(input, now));
  for (const component of Object.keys(healthy.checks)) {
    for (const value of [false, undefined, "true", 1]) assert.throws(() => validateHostStatus({ ...healthy, checks: { ...healthy.checks, [component]: value } }, now));
  }
});

test("an optional connection pin accepts only literal addresses and preserves normal TLS hostname verification", () => {
  assert.deepEqual(hostConnection(), {});
  for (const value of ["other.example", "127.0.0.1:8443", "127.0.0.1\nmalformed"]) assert.throws(() => hostConnection(value));
  for (const address of ["192.0.2.1", "2001:db8::1"]) {
    const options = hostConnection(address);
    assert.deepEqual(Object.keys(options), ["lookup"]);
    options.lookup!("original.example", { all: true }, (error, addresses) => {
      assert.equal(error, null); assert.deepEqual(addresses, [{ address, family: address.includes(":") ? 6 : 4 }]);
    });
  }
});
