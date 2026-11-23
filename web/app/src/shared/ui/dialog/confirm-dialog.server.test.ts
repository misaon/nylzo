import { render } from 'svelte/server';
import { describe, expect, it, vi } from 'vitest';

import ConfirmDialog from './ConfirmDialog.svelte';

describe('confirm dialog on the server', () => {
  it('ties its message to the dialog and marks a dangerous confirmation with the default icon', () => {
    const { body } = render(ConfirmDialog, {
      props: {
        id: 'confirm',
        title: 'Title',
        message: 'Why',
        confirmLabel: 'Delete',
        dismissLabel: 'Keep',
        onconfirm: vi.fn<() => void>(),
        onclose: vi.fn<() => void>(),
      },
    });

    expect(body).toContain('aria-labelledby="confirm"');
    expect(body).toContain('aria-describedby="confirm-message"');
    expect(body).toContain('id="confirm-message"');
    expect(body).toContain('tile-danger');
    expect(body.match(/<svg/gu)).toHaveLength(2);
  });
});
