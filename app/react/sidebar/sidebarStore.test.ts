import { beforeEach, describe, expect, it } from 'vitest';

import {
  applyAppearance,
  defaultAppearance,
} from '@/react/portainer/appearance/appearance';

import { applyDefaultSidebarState, sidebarStore } from './sidebarStore';

describe('sidebar appearance default', () => {
  beforeEach(() => {
    localStorage.removeItem('portainer.toolbar_toggle');
    window.innerWidth = 1440;
    delete (window as Window & { ddExtension?: boolean }).ddExtension;
    sidebarStore.getState().setOpen(true);
    applyAppearance(defaultAppearance);
  });

  it('uses the configured default when no personal sidebar choice is saved', () => {
    applyAppearance({
      ...defaultAppearance,
      Layout: { ...defaultAppearance.Layout, SidebarDefault: 'closed' },
    });

    applyDefaultSidebarState();

    expect(sidebarStore.getState().isOpen).toBe(false);
  });

  it('preserves a saved personal sidebar choice when the system default changes', () => {
    localStorage.setItem('portainer.toolbar_toggle', JSON.stringify(false));
    applyAppearance(defaultAppearance);

    sidebarStore.getState().setOpen(true);
    applyDefaultSidebarState();

    expect(sidebarStore.getState().isOpen).toBe(true);
  });
});
