export type JsonObject = Readonly<Record<string, unknown>>;

export function objectAt(value: unknown, location: string): JsonObject {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new TypeError(`${location} must be an object`);
  }
  return value as JsonObject;
}

export function arrayAt(value: unknown, location: string): readonly unknown[] {
  if (!Array.isArray(value)) {
    throw new TypeError(`${location} must be an array`);
  }
  return value;
}

export function optionalArrayAt(value: unknown, location: string): readonly unknown[] {
  return value === undefined || value === null ? [] : arrayAt(value, location);
}

export function textAt(value: unknown, location: string): string {
  if (typeof value !== 'string' || value === '') {
    throw new TypeError(`${location} must be a non-empty string`);
  }
  return value;
}

export function textsAt(value: unknown, location: string): string[] {
  return arrayAt(value, location).map((item, index) => textAt(item, `${location}[${index}]`));
}
