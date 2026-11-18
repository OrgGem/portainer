import logoLight from '@/assets/images/logo_alt_black.svg';
import logoDark from '@/assets/images/logo_alt.svg';
import { ThemeColor } from '@/portainer/users/types';

export interface AppearancePalette {
  Accent: string;
  PageBackground: string;
  SurfaceBackground: string;
  SidebarBackground: string;
}

export interface Appearance {
  Branding: { Name: string; FaviconURL: string };
  Colors: { Light: AppearancePalette; Dark: AppearancePalette };
  Typography: {
    FontFamily: 'inter' | 'system' | 'arial';
    FontScale: 100 | 110 | 120;
  };
  Layout: {
    SidebarWidth: number;
    SidebarDefault: 'open' | 'closed';
    Density: 'standard' | 'compact';
  };
  DefaultTheme: ThemeColor;
}

const emptyPalette: AppearancePalette = {
  Accent: '',
  PageBackground: '',
  SurfaceBackground: '',
  SidebarBackground: '',
};

export const defaultAppearance: Appearance = {
  Branding: { Name: 'Portainer', FaviconURL: '' },
  Colors: { Light: { ...emptyPalette }, Dark: { ...emptyPalette } },
  Typography: { FontFamily: 'inter', FontScale: 100 },
  Layout: { SidebarWidth: 300, SidebarDefault: 'open', Density: 'standard' },
  DefaultTheme: 'auto',
};

export function normalizeAppearance(
  appearance?: Appearance | null
): Appearance {
  return {
    Branding: { ...defaultAppearance.Branding, ...appearance?.Branding },
    Colors: {
      Light: { ...emptyPalette, ...appearance?.Colors?.Light },
      Dark: { ...emptyPalette, ...appearance?.Colors?.Dark },
    },
    Typography: { ...defaultAppearance.Typography, ...appearance?.Typography },
    Layout: { ...defaultAppearance.Layout, ...appearance?.Layout },
    DefaultTheme: appearance?.DefaultTheme || 'auto',
  };
}

let currentAppearance = normalizeAppearance();
let themeRefresh: (() => void) | undefined;
let initialIcons:
  | Array<{ element: HTMLLinkElement; href: string | null }>
  | undefined;
let logoFallbackInstalled = false;

export function getDefaultTheme() {
  return currentAppearance.DefaultTheme;
}

export function getAppearance() {
  return currentAppearance;
}

export function registerAppearanceThemeChange(callback: () => void) {
  themeRefresh = callback;
}

// CSS colors are only set when explicitly configured, preserving stock appearance.
export function appearanceThemeChanged() {
  const root = document.documentElement;
  const palette =
    root.getAttribute('theme') === 'dark'
      ? currentAppearance.Colors.Dark
      : currentAppearance.Colors.Light;
  const highContrast = root.getAttribute('theme') === 'highcontrast';
  const fallbackLogo =
    root.getAttribute('theme') === 'dark' || highContrast
      ? logoDark
      : logoLight;
  document
    .querySelectorAll<HTMLImageElement>(
      'img.appearance-logo[data-appearance-fallback]'
    )
    .forEach((element) => {
      const image = element;
      image.src = fallbackLogo;
    });
  function color(value: string) {
    return !highContrast && /^#[\da-f]{6}$/i.test(value) ? value : '';
  }
  const accent = color(palette.Accent);
  const page = color(palette.PageBackground);
  const surface = color(palette.SurfaceBackground);
  const sidebar = color(palette.SidebarBackground);

  for (const name of [
    '--appearance-accent',
    '--appearance-accent-text',
    '--appearance-accent-page',
    '--appearance-accent-surface',
    '--appearance-accent-secondary',
    '--appearance-accent-hover',
    '--appearance-accent-active',
    '--appearance-page-text',
    '--appearance-surface-text',
    '--appearance-sidebar-text',
    '--appearance-sidebar-hover',
    '--bg-body-color',
    '--bg-widget-color',
    '--bg-widget-header-color',
    '--bg-widget-table-color',
    '--bg-panel-body-color',
    '--bg-table-color',
    '--bg-hover-table-color',
    '--bg-table-selected-color',
    '--bg-card-color',
    '--bg-dropdown-menu-color',
    '--bg-dropdown-hover',
    '--text-dropdown-menu-color',
    '--text-widget-header-color',
    '--user-menu-icon-color',
    '--bg-modal-content-color',
    '--bg-inputbox',
    '--bg-sidebar-color',
    '--bg-sidebar-nav-color',
  ]) {
    root.style.removeProperty(name);
  }
  root.toggleAttribute('data-appearance-accent', !!accent);
  root.toggleAttribute('data-appearance-page', !!page);
  root.toggleAttribute('data-appearance-surface', !!surface);
  root.toggleAttribute('data-appearance-sidebar', !!sidebar);
  if (accent) {
    const dark = root.getAttribute('theme') === 'dark';
    const pageBackground = page || (dark ? '#121212' : '#fdfdfd');
    const surfaceBackground = surface || (dark ? '#212121' : '#ffffff');
    root.style.setProperty('--appearance-accent', accent);
    const accentText = contrastText(accent);
    root.style.setProperty('--appearance-accent-text', accentText);
    root.style.setProperty(
      '--appearance-accent-page',
      readableAccent(accent, pageBackground)
    );
    root.style.setProperty(
      '--appearance-accent-surface',
      readableAccent(accent, surfaceBackground)
    );
    root.style.setProperty(
      '--appearance-accent-secondary',
      readableAccent(accent, dark ? '#2e2f33' : '#f7f6f3')
    );
    const awayFromText = accentText === '#000000' ? '#ffffff' : '#000000';
    root.style.setProperty(
      '--appearance-accent-hover',
      mix(accent, awayFromText, 0.16)
    );
    root.style.setProperty(
      '--appearance-accent-active',
      mix(accent, awayFromText, 0.25)
    );
  }
  if (page) {
    root.style.setProperty('--bg-body-color', page);
    root.style.setProperty('--appearance-page-text', contrastText(page));
  }
  if (surface) {
    for (const name of [
      '--bg-widget-color',
      '--bg-widget-header-color',
      '--bg-widget-table-color',
      '--bg-panel-body-color',
      '--bg-table-color',
      '--bg-card-color',
      '--bg-dropdown-menu-color',
      '--bg-modal-content-color',
      '--bg-inputbox',
    ])
      root.style.setProperty(name, surface);
    const surfaceText = contrastText(surface);
    root.style.setProperty('--appearance-surface-text', surfaceText);
    root.style.setProperty('--text-dropdown-menu-color', surfaceText);
    root.style.setProperty('--text-widget-header-color', surfaceText);
    root.style.setProperty(
      '--bg-dropdown-hover',
      surfaceState(surface, surfaceText, 0.08)
    );
    root.style.setProperty(
      '--user-menu-icon-color',
      surfaceState(surface, surfaceText, 0.08)
    );
    root.style.setProperty(
      '--bg-hover-table-color',
      surfaceState(surface, surfaceText, 0.08)
    );
    root.style.setProperty(
      '--bg-table-selected-color',
      surfaceState(surface, surfaceText, 0.12)
    );
  }
  if (sidebar) {
    root.style.setProperty('--bg-sidebar-color', sidebar);
    root.style.setProperty('--bg-sidebar-nav-color', sidebar);
    const sidebarText = contrastText(sidebar);
    root.style.setProperty('--appearance-sidebar-text', sidebarText);
    root.style.setProperty(
      '--appearance-sidebar-hover',
      surfaceState(sidebar, sidebarText, 0.12)
    );
  }
}

export function contrastText(hex: string) {
  return luminance(hex) > 0.179 ? '#000000' : '#ffffff';
}

function luminance(hex: string) {
  const channels = [1, 3, 5]
    .map((index) => parseInt(hex.slice(index, index + 2), 16) / 255)
    .map((value) =>
      value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
    );
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
}

function contrastRatio(first: string, second: string) {
  const a = luminance(first);
  const b = luminance(second);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

function surfaceState(surface: string, foreground: string, amount: number) {
  const candidate = mix(surface, foreground, amount);
  return contrastRatio(candidate, foreground) >= 4.5 ? candidate : surface;
}

export function readableAccent(accent: string, background: string) {
  const target = contrastText(background);
  for (let amount = 0; amount <= 1.001; amount += 0.05) {
    const candidate = mix(accent, target, amount);
    if (contrastRatio(candidate, background) >= 4.5) return candidate;
  }
  return target;
}

function mix(hex: string, target: string, amount: number) {
  return `#${[1, 3, 5]
    .map((index) =>
      Math.round(
        parseInt(hex.slice(index, index + 2), 16) * (1 - amount) +
          parseInt(target.slice(index, index + 2), 16) * amount
      )
        .toString(16)
        .padStart(2, '0')
    )
    .join('')}`;
}

function applyFavicon(url: string) {
  if (!initialIcons) {
    initialIcons = Array.from(
      document.querySelectorAll<HTMLLinkElement>(
        'link[rel~="icon"], link[rel="apple-touch-icon"], link[rel="mask-icon"]'
      )
    ).map((element) => ({ element, href: element.getAttribute('href') }));
  }
  for (const { element, href } of initialIcons) {
    element.onerror = null;
    if (url) {
      element.onerror = () => {
        element.onerror = null;
        if (href === null) element.removeAttribute('href');
        else element.setAttribute('href', href);
      };
      element.setAttribute('href', url);
    } else if (href === null) element.removeAttribute('href');
    else element.setAttribute('href', href);
  }
}

export function applyAppearance(appearance?: Appearance | null) {
  currentAppearance = normalizeAppearance(appearance);
  const root = document.documentElement;
  const { Branding, Typography, Layout } = currentAppearance;
  document.title = Branding.Name || 'Portainer';
  applyFavicon(Branding.FaviconURL);
  const fonts = {
    inter: '',
    system: 'system-ui, -apple-system, sans-serif',
    arial: 'Arial, Helvetica, sans-serif',
  };
  const font = fonts[Typography.FontFamily] || '';
  if (font) root.style.setProperty('--appearance-font-family', font);
  else root.style.removeProperty('--appearance-font-family');
  root.toggleAttribute(
    'data-appearance-font-scale',
    Typography.FontScale !== 100
  );
  if (Typography.FontScale !== 100) {
    root.style.setProperty(
      'font-size',
      `${(16 * Typography.FontScale) / 100}px`
    );
    root.style.setProperty(
      '--appearance-font-scale',
      `${Typography.FontScale / 100}`
    );
  } else {
    root.style.removeProperty('font-size');
    root.style.removeProperty('--appearance-font-scale');
  }
  if (Layout.SidebarWidth !== 300)
    root.style.setProperty(
      '--appearance-sidebar-width',
      `${Layout.SidebarWidth}px`
    );
  else root.style.removeProperty('--appearance-sidebar-width');
  root.toggleAttribute('data-appearance-compact', Layout.Density === 'compact');
  if (!logoFallbackInstalled) {
    window.addEventListener(
      'error',
      (event) => {
        const image = event.target;
        if (
          image instanceof HTMLImageElement &&
          image.classList.contains('appearance-logo') &&
          !image.dataset.appearanceFallback
        ) {
          image.dataset.appearanceFallback = 'true';
          image.src =
            document.documentElement.getAttribute('theme') === 'dark' ||
            document.documentElement.getAttribute('theme') === 'highcontrast'
              ? logoDark
              : logoLight;
        }
      },
      true
    );
    logoFallbackInstalled = true;
  }
  themeRefresh?.();
  appearanceThemeChanged();
  window.dispatchEvent(new Event('portainer:appearance-changed'));
}
