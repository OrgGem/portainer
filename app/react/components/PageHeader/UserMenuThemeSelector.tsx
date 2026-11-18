import { Eye, Moon, RefreshCw, Sun } from 'lucide-react';
import { ReactNode } from 'react';

import { User, ThemeColor } from '@/portainer/users/types';
import { applyTheme } from '@/react/portainer/services/applyTheme';
import { useUpdateUserMutation } from '@/react/portainer/account/useUpdateUserMutation';
import { options as themeOptions } from '@/react/portainer/account/AccountView/theme-options';
import { usePublicSettings } from '@/react/portainer/settings/queries';

import {
  SegmentItem,
  SegmentedControl,
} from '@@/form-components/SegmentedControl';
import { Icon } from '@@/Icon';

const themeIconMap: Record<ThemeColor, ReactNode> = {
  light: <Icon icon={Sun} />,
  dark: <Icon icon={Moon} />,
  highcontrast: <Icon icon={Eye} />,
  auto: <Icon icon={RefreshCw} />,
};

const themeSegmentItems: SegmentItem[] = themeOptions.map((option) => ({
  id: option.id,
  label: themeIconMap[option.id],
  title: option.label,
}));

export function ThemeSelector({ user }: { user: User }) {
  const updateMutation = useUpdateUserMutation();
  const settings = usePublicSettings();
  const activeTheme = user?.ThemeSettings?.color || settings.data?.Appearance?.DefaultTheme || 'auto';

  function handleThemeSelect(id: string) {
    const color = id as ThemeColor;
    applyTheme(color);
    updateMutation.mutate({ theme: { color } });
  }

  return (
    <SegmentedControl
      label="Theme"
      items={themeSegmentItems}
      activeId={activeTheme}
      onChange={handleThemeSelect}
      size="md"
    />
  );
}
