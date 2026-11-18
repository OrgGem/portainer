import { ThemeColor } from '@/portainer/users/types';
import { appearanceThemeChanged, getDefaultTheme, registerAppearanceThemeChange } from '@/react/portainer/appearance/appearance';

let removeAutoThemeListener: (() => void) | null = null;
let selectedTheme: ThemeColor | undefined;

export function applyTheme(preference?: ThemeColor) {
  selectedTheme = preference;
  const color = preference || getDefaultTheme();
  removeAutoThemeListener?.();
  removeAutoThemeListener = null;

  if (color === 'auto' || !color) {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');

    function handleChange() {
      applyAutoTheme(mediaQuery);
    }

    applyAutoTheme(mediaQuery);
    mediaQuery.addEventListener('change', handleChange);
    removeAutoThemeListener = () =>
      mediaQuery.removeEventListener('change', handleChange);
  } else {
    document.documentElement.setAttribute('theme', color);
  }
  appearanceThemeChanged();
}

registerAppearanceThemeChange(() => applyTheme(selectedTheme));

function applyAutoTheme(mediaQuery: MediaQueryList) {
  if (mediaQuery.matches) {
    document.documentElement.setAttribute('theme', 'dark');
  } else {
    document.documentElement.removeAttribute('theme');
  }
  appearanceThemeChanged();
}
