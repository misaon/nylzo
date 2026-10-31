interface TrustedScriptURL {
  readonly brand: unique symbol;
}

interface TrustedTypePolicy {
  createScriptURL(input: string): TrustedScriptURL;
}

interface TrustedTypePolicyFactory {
  createPolicy(name: string, rules: { createScriptURL(input: string): string }): TrustedTypePolicy;
}

declare const trustedTypes: TrustedTypePolicyFactory;
