import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { Settings } from '@/react/portainer/settings/types';

import { AppearancePanel } from './AppearancePanel';
import { Appearance, defaultAppearance } from './appearance';

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock('@/react/portainer/settings/queries', () => ({
  useUpdateSettingsMutation: () => ({ mutate, isLoading: false }),
}));

vi.mock('@/portainer/services/notifications', () => ({
  notifySuccess: vi.fn(),
}));

function makeSettings(): Settings {
  return {
    LogoURL: '',
    Appearance: structuredClone(defaultAppearance),
  } as Settings;
}

describe('AppearancePanel', () => {
  beforeEach(() => {
    mutate.mockReset();
    document.title = 'Portainer';
    document.documentElement.style.cssText = '';
  });

  it('keeps preview edits local until saving and restores them on cancel', () => {
    render(<AppearancePanel settings={makeSettings()} />);

    const brandName = screen.getByLabelText('Brand name');
    fireEvent.change(brandName, { target: { value: 'Acme' } });

    const preview = screen.getByRole('region', { name: 'Light appearance preview' });
    expect(within(preview).getByText('Acme')).toBeInTheDocument();
    expect(document.title).toBe('Portainer');
    expect(document.documentElement.style.getPropertyValue('--appearance-accent')).toBe('');
    expect(mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByLabelText('Brand name')).toHaveValue('Portainer');
    expect(within(preview).getByText('Portainer')).toBeInTheDocument();
  });

  it('saves the complete appearance and logo together', () => {
    render(<AppearancePanel settings={makeSettings()} />);

    fireEvent.change(screen.getByLabelText('Brand name'), {
      target: { value: 'Acme Platform' },
    });
    fireEvent.change(screen.getByLabelText('Logo URL'), {
      target: { value: 'https://assets.example.test/logo.png' },
    });
    fireEvent.change(screen.getByLabelText('Font'), {
      target: { value: 'system' },
    });
    fireEvent.change(screen.getByLabelText('Font size'), {
      target: { value: '120' },
    });
    fireEvent.change(screen.getByLabelText('Sidebar width (px)'), {
      target: { value: '320' },
    });
    fireEvent.change(screen.getByLabelText('Default theme'), {
      target: { value: 'dark' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save appearance' }));

    const [payload] = mutate.mock.calls[0];
    expect(payload.LogoURL).toBe('https://assets.example.test/logo.png');
    expect(payload.Appearance).toMatchObject({
      Branding: { Name: 'Acme Platform' },
      Typography: { FontFamily: 'system', FontScale: 120 },
      Layout: { SidebarWidth: 320 },
      DefaultTheme: 'dark',
    });
  });

  it('refreshes the pristine draft when settings are refetched', () => {
    const initialSettings = makeSettings();
    const { rerender } = render(
      <AppearancePanel settings={initialSettings} />
    );

    const latestSettings = makeSettings();
    latestSettings.LogoURL = 'https://assets.example.test/latest-logo.png';
    latestSettings.Appearance!.Branding.Name = 'Latest server value';
    rerender(<AppearancePanel settings={latestSettings} />);

    expect(screen.getByLabelText('Brand name')).toHaveValue(
      'Latest server value'
    );
    expect(screen.getByLabelText('Logo URL')).toHaveValue(
      'https://assets.example.test/latest-logo.png'
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('preserves a dirty draft, blocks a stale save, and reloads current settings on request', () => {
    const initialSettings = makeSettings();
    const { rerender } = render(
      <AppearancePanel settings={initialSettings} />
    );
    fireEvent.change(screen.getByLabelText('Brand name'), {
      target: { value: 'Unsaved draft' },
    });

    const latestSettings = makeSettings();
    latestSettings.LogoURL = 'https://assets.example.test/latest-logo.png';
    latestSettings.Appearance!.Branding.Name = 'Another admin';
    rerender(<AppearancePanel settings={latestSettings} />);

    expect(screen.getByLabelText('Brand name')).toHaveValue('Unsaved draft');
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save appearance' })).toBeDisabled();
    fireEvent.click(
      screen.getByRole('button', { name: 'Save appearance' })
    );
    expect(mutate).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole('button', { name: 'Reload saved appearance' })
    );
    expect(screen.getByLabelText('Brand name')).toHaveValue('Another admin');
    expect(screen.getByLabelText('Logo URL')).toHaveValue(
      'https://assets.example.test/latest-logo.png'
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save appearance' })).toBeEnabled();
  });

  it('treats the saved response as the new clean baseline after an appearance save', () => {
    const initialSettings = makeSettings();
    const { rerender } = render(
      <AppearancePanel settings={initialSettings} />
    );
    fireEvent.change(screen.getByLabelText('Brand name'), {
      target: { value: 'Acme' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save appearance' }));

    const [payload, mutationOptions] = mutate.mock.calls[0];
    const savedSettings = {
      ...initialSettings,
      ...payload,
    } as Settings;
    mutationOptions.onSuccess(savedSettings);
    rerender(<AppearancePanel settings={savedSettings} />);

    expect(screen.getByLabelText('Brand name')).toHaveValue('Acme');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save appearance' })).toBeEnabled();
  });

  it('keeps reset as an unsaved draft and disables save for invalid colors', () => {
    const settings = makeSettings();
    settings.LogoURL = 'https://assets.example.test/logo.png';
    render(<AppearancePanel settings={settings} />);

    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }));
    expect(screen.getByLabelText('Brand name')).toHaveValue('Portainer');
    expect(screen.getByLabelText('Logo URL')).toHaveValue('');
    expect(mutate).not.toHaveBeenCalled();

    cleanup();
    render(
      <AppearancePanel
        settings={{
          ...settings,
          Appearance: {
            ...defaultAppearance,
            Colors: {
              Light: {
                ...defaultAppearance.Colors.Light,
                Accent: 'red',
              },
              Dark: { ...defaultAppearance.Colors.Dark },
            },
          } as Appearance,
        }}
      />
    );

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Colors must be empty or use #RRGGBB.'
    );
    expect(screen.getByRole('button', { name: 'Save appearance' })).toBeDisabled();
    expect(mutate).not.toHaveBeenCalled();
  });
});
