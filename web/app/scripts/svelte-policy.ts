import { parse } from 'svelte/compiler';

// Oxlint currently inspects script blocks only. Keep markup security checks on Svelte's AST.
export function sveltePolicy(source: string): string[] {
  const problems: string[] = [];
  const root = parse(source, { modern: true });
  function visit(value: unknown): void {
    if (Array.isArray(value)) {
      for (const child of value) {
        visit(child);
      }
      return;
    }
    if (typeof value !== 'object' || value === null) {
      return;
    }
    const type: unknown = Reflect.get(value, 'type');
    const name: unknown = Reflect.get(value, 'name');
    if (type === 'HtmlTag' || type === 'ConstTag') {
      problems.push(`${type} is forbidden in templates`);
    }
    if (type === 'BindDirective' && name === 'innerHTML') {
      problems.push('bind:innerHTML parses markup');
    }
    if (type === 'StyleDirective' || (type === 'Attribute' && name === 'style')) {
      problems.push('Use stylesheet classes instead of inline styles');
    }
    if (type === 'RegularElement' && name === 'button') {
      const attributes: unknown = Reflect.get(value, 'attributes');
      if (
        !Array.isArray(attributes) ||
        !attributes.some(
          (attribute: unknown) =>
            typeof attribute === 'object' && attribute !== null && Reflect.get(attribute, 'name') === 'type',
        )
      ) {
        problems.push('Every button needs an explicit type');
      }
    }
    if (type === 'Script') {
      const attributes: unknown = Reflect.get(value, 'attributes');
      const typed =
        Array.isArray(attributes) &&
        attributes.some((attribute: unknown) => {
          if (typeof attribute !== 'object' || attribute === null || Reflect.get(attribute, 'name') !== 'lang') {
            return false;
          }
          const content: unknown = Reflect.get(attribute, 'value');
          return (
            Array.isArray(content) &&
            content.some(
              (text: unknown) => typeof text === 'object' && text !== null && Reflect.get(text, 'data') === 'ts',
            )
          );
        });
      if (!typed) {
        problems.push('Every script must declare lang="ts"');
      }
    }
    const children: unknown[] = Object.values(value);
    for (const child of children) {
      visit(child);
    }
  }
  visit(root);
  return problems;
}
