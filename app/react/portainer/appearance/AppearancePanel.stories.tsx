import type { Meta, StoryObj } from '@storybook/react-webpack5';

import { Settings } from '@/react/portainer/settings/types';

import { AppearancePanel } from './AppearancePanel';
import { defaultAppearance } from './appearance';

const meta: Meta<typeof AppearancePanel> = {
  title: 'Portainer/Settings/Appearance',
  component: AppearancePanel,
  args: {
    settings: makeSettings(),
  },
};

export default meta;
type Story = StoryObj<typeof AppearancePanel>;

export const Default: Story = {};

export const LongBrandName: Story = {
  args: {
    settings: makeSettings({
      ...defaultAppearance,
      Branding: {
        ...defaultAppearance.Branding,
        Name: 'Gem Labs Container Platform · Central Operations',
      },
    }),
  },
};

export const ExtremeColorContrast: Story = {
  args: {
    settings: makeSettings({
      ...defaultAppearance,
      Colors: {
        Light: {
          Accent: '#FFFFFF',
          PageBackground: '#FFFFFF',
          SurfaceBackground: '#000000',
          SidebarBackground: '#FFFFFF',
        },
        Dark: {
          Accent: '#000000',
          PageBackground: '#000000',
          SurfaceBackground: '#000000',
          SidebarBackground: '#000000',
        },
      },
    }),
  },
};

function makeSettings(appearance = structuredClone(defaultAppearance)): Settings {
  return {
    LogoURL: '',
    Appearance: appearance,
  } as Settings;
}
