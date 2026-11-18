import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import {
  applyAppearance,
  appearanceThemeChanged,
  contrastText,
  defaultAppearance,
  getDefaultTheme,
  normalizeAppearance,
  readableAccent,
} from './appearance';

const root = document.documentElement;
let favicon: HTMLLinkElement;

beforeAll(() => {
  favicon = document.createElement('link');
  favicon.rel = 'icon';
  favicon.href = '/favicon.ico';
  document.head.append(favicon);
});

beforeEach(() => {
  root.removeAttribute('theme');
  root.style.cssText = '';
  root.removeAttribute('data-appearance-accent');
  root.removeAttribute('data-appearance-page');
  root.removeAttribute('data-appearance-surface');
  root.removeAttribute('data-appearance-sidebar');
  root.removeAttribute('data-appearance-compact');
  document.title = 'Portainer';
  favicon.href = '/favicon.ico';
});

afterEach(() => {
  applyAppearance(defaultAppearance);
  root.removeAttribute('theme');
  root.style.cssText = '';
});

describe('Appearance', () => {
  it('normalizes missing persisted fields against the legacy defaults', () => {
    expect(normalizeAppearance()).toEqual(defaultAppearance);
    expect(
      normalizeAppearance({
        ...defaultAppearance,
        Branding: { ...defaultAppearance.Branding, Name: 'Acme' },
        Colors: { Light: { Accent: '#123456' }, Dark: {} } as never,
      })
    ).toMatchObject({
      Branding: { Name: 'Acme', FaviconURL: '' },
      Colors: {
        Light: {
          Accent: '#123456',
          PageBackground: '',
          SurfaceBackground: '',
          SidebarBackground: '',
        },
        Dark: {
          Accent: '',
          PageBackground: '',
          SurfaceBackground: '',
          SidebarBackground: '',
        },
      },
      Typography: { FontFamily: 'inter', FontScale: 100 },
      Layout: { SidebarWidth: 300, SidebarDefault: 'open', Density: 'standard' },
      DefaultTheme: 'auto',
    });
  });

  it('applies branding, favicon, typography, and layout settings', () => {
    applyAppearance({
      ...defaultAppearance,
      Branding: { Name: 'Acme Platform', FaviconURL: '/assets/acme.svg' },
      Typography: { FontFamily: 'system', FontScale: 120 },
      Layout: { SidebarWidth: 320, SidebarDefault: 'closed', Density: 'compact' },
    });

    expect(document.title).toBe('Acme Platform');
    expect(favicon.getAttribute('href')).toBe('/assets/acme.svg');
    expect(root.style.getPropertyValue('--appearance-font-family')).toBe(
      'system-ui, -apple-system, sans-serif'
    );
    expect(root.style.getPropertyValue('font-size')).toBe('19.2px');
    expect(root.style.getPropertyValue('--appearance-sidebar-width')).toBe(
      '320px'
    );
    expect(root).toHaveAttribute('data-appearance-compact');
  });

  it('restores the built-in brand and browser resources on reset', () => {
    applyAppearance({
      ...defaultAppearance,
      Branding: { Name: 'Acme', FaviconURL: 'https://example.test/acme.ico' },
      Typography: { FontFamily: 'arial', FontScale: 110 },
      Layout: { SidebarWidth: 280, SidebarDefault: 'closed', Density: 'compact' },
    });

    applyAppearance(defaultAppearance);

    expect(document.title).toBe('Portainer');
    expect(favicon.getAttribute('href')).toBe('/favicon.ico');
    expect(root.style.getPropertyValue('--appearance-font-family')).toBe('');
    expect(root.style.getPropertyValue('font-size')).toBe('');
    expect(root.style.getPropertyValue('--appearance-sidebar-width')).toBe('');
    expect(root).not.toHaveAttribute('data-appearance-compact');
  });

  it('applies the active light or dark palette and preserves high contrast', () => {
    const appearance = {
      ...defaultAppearance,
      Colors: {
        Light: {
          Accent: '#326CE5',
          PageBackground: '#F5F5F5',
          SurfaceBackground: '#FFFFFF',
          SidebarBackground: '#222222',
        },
        Dark: {
          Accent: '#22AA88',
          PageBackground: '#111111',
          SurfaceBackground: '#222222',
          SidebarBackground: '#000000',
        },
      },
    };

    root.setAttribute('theme', 'light');
    applyAppearance(appearance);
    expect(root.style.getPropertyValue('--appearance-accent')).toBe('#326CE5');
    expect(root.style.getPropertyValue('--appearance-page-text')).toBe('#000000');

    root.setAttribute('theme', 'dark');
    appearanceThemeChanged();
    expect(root.style.getPropertyValue('--appearance-accent')).toBe('#22AA88');
    expect(root.style.getPropertyValue('--bg-body-color')).toBe('#111111');

    root.setAttribute('theme', 'highcontrast');
    appearanceThemeChanged();
    expect(root.style.getPropertyValue('--appearance-accent')).toBe('');
    expect(root).not.toHaveAttribute('data-appearance-accent');
  });

  it('exposes the configured theme default and computes readable contrast text', () => {
    applyAppearance({ ...defaultAppearance, DefaultTheme: 'dark' });

    expect(getDefaultTheme()).toBe('dark');
    expect(contrastText('#ffffff')).toBe('#000000');
    expect(contrastText('#000000')).toBe('#ffffff');
  });

  it('keeps accent text readable against very light and dark page and surface colors', () => {
    const appearance = {
      ...defaultAppearance,
      Colors: {
        Light: {
          Accent: '#ffffff',
          PageBackground: '#ffffff',
          SurfaceBackground: '#000000',
          SidebarBackground: '#222222',
        },
        Dark: {
          Accent: '#000000',
          PageBackground: '#000000',
          SurfaceBackground: '#000000',
          SidebarBackground: '#000000',
        },
      },
    };

    root.setAttribute('theme', 'light');
    applyAppearance(appearance);
    expect(
      contrastRatio(
        root.style.getPropertyValue('--appearance-accent-page'),
        '#ffffff'
      )
    ).toBeGreaterThanOrEqual(4.5);
    expect(
      contrastRatio(
        root.style.getPropertyValue('--appearance-accent-surface'),
        '#000000'
      )
    ).toBeGreaterThanOrEqual(4.5);

    root.setAttribute('theme', 'dark');
    appearanceThemeChanged();
    expect(
      contrastRatio(
        root.style.getPropertyValue('--appearance-accent-page'),
        '#000000'
      )
    ).toBeGreaterThanOrEqual(4.5);
    expect(
      contrastRatio(
        root.style.getPropertyValue('--appearance-accent-surface'),
        '#000000'
      )
    ).toBeGreaterThanOrEqual(4.5);

    expect(contrastRatio(readableAccent('#ffffff', '#ffffff'), '#ffffff')).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(readableAccent('#000000', '#000000'), '#000000')).toBeGreaterThanOrEqual(4.5);
  });

  it('provides contrast tokens for custom dark surfaces and removes them in high contrast', () => {
    const appearance = {
      ...defaultAppearance,
      Colors: {
        Light: { ...defaultAppearance.Colors.Light },
        Dark: {
          Accent: '#326CE5',
          PageBackground: '#FFFFFF',
          SurfaceBackground: '#FFFFFF',
          SidebarBackground: '#FFFFFF',
        },
      },
    };

    root.setAttribute('theme', 'dark');
    applyAppearance(appearance);

    expect(root).toHaveAttribute('data-appearance-surface');
    expect(root).toHaveAttribute('data-appearance-sidebar');
    expect(root.style.getPropertyValue('--bg-inputbox')).toBe('#FFFFFF');
    expect(root.style.getPropertyValue('--appearance-surface-text')).toBe(
      '#000000'
    );
    expect(root.style.getPropertyValue('--appearance-sidebar-text')).toBe(
      '#000000'
    );
    expect(root.style.getPropertyValue('--appearance-sidebar-hover')).not.toBe(
      '#FFFFFF'
    );

    root.setAttribute('theme', 'highcontrast');
    appearanceThemeChanged();
    expect(root).not.toHaveAttribute('data-appearance-surface');
    expect(root).not.toHaveAttribute('data-appearance-sidebar');
    expect(root.style.getPropertyValue('--bg-inputbox')).toBe('');
  });
});

function contrastRatio(first: string, second: string) {
  const firstLuminance = luminance(first);
  const secondLuminance = luminance(second);
  return (
    (Math.max(firstLuminance, secondLuminance) + 0.05) /
    (Math.min(firstLuminance, secondLuminance) + 0.05)
  );
}

function luminance(hex: string) {
  const values = [1, 3, 5].map((index) => parseInt(hex.slice(index, index + 2), 16) / 255);
  const linear = values.map((value) =>
    value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  );
  return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
}
