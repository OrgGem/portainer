import angular from 'angular';
import { Palette } from 'lucide-react';
import { FormEvent, useState } from 'react';

import { notifySuccess } from '@/portainer/services/notifications';
import { StateManager } from '@/portainer/services/types';
import { useUpdateSettingsMutation } from '@/react/portainer/settings/queries';
import { Settings } from '@/react/portainer/settings/types';

import { LoadingButton, Button } from '@@/buttons';
import { isValidUrl } from '@@/form-components/validate-url';
import { Widget } from '@@/Widget';

import {
  Appearance,
  AppearancePalette,
  contrastText,
  defaultAppearance,
  normalizeAppearance,
} from './appearance';

type FormValues = { appearance: Appearance; logo: string };
type Mode = 'Light' | 'Dark';
const paletteFields: Array<[keyof AppearancePalette, string]> = [
  ['Accent', 'Accent / buttons'],
  ['PageBackground', 'Page background'],
  ['SurfaceBackground', 'Cards and content'],
  ['SidebarBackground', 'Sidebar background'],
];

function validate(values: FormValues) {
  const { appearance, logo } = values;
  if (Array.from(appearance.Branding.Name).length > 80)
    return 'Brand name must be at most 80 characters.';
  if (logo && !isValidUrl(logo)) return 'Logo must be a valid URL.';
  const favicon = appearance.Branding.FaviconURL;
  if (favicon) {
    try {
      if (favicon.includes('\\') || favicon.startsWith('//'))
        throw new Error('Invalid URL');
      const parsed = new URL(favicon, window.location.origin);
      if (
        !['http:', 'https:'].includes(parsed.protocol) ||
        parsed.username ||
        parsed.password ||
        (!favicon.startsWith('/') && !/^https?:\/\//i.test(favicon))
      )
        throw new Error('Invalid URL');
    } catch {
      return 'Favicon must be an HTTP(S) URL or an absolute path on this site.';
    }
  }
  for (const palette of [appearance.Colors.Light, appearance.Colors.Dark]) {
    if (
      Object.values(palette).some(
        (color) => color && !/^#[\da-f]{6}$/i.test(color)
      )
    )
      return 'Colors must be empty or use #RRGGBB.';
  }
  if (
    !Number.isInteger(appearance.Layout.SidebarWidth) ||
    appearance.Layout.SidebarWidth < 240 ||
    appearance.Layout.SidebarWidth > 360
  )
    return 'Sidebar width must be between 240 and 360 px.';
  return '';
}

function formValuesFromSettings(settings: Settings): FormValues {
  return {
    appearance: normalizeAppearance(settings.Appearance),
    logo: settings.LogoURL || '',
  };
}

function valuesKey(values: FormValues) {
  return JSON.stringify(values);
}

export function AppearancePanel({
  settings,
  isRefreshing = false,
}: {
  settings: Settings;
  isRefreshing?: boolean;
}) {
  const incoming = formValuesFromSettings(settings);
  const incomingKey = valuesKey(incoming);
  const [seenKey, setSeenKey] = useState(incomingKey);
  const [baseline, setBaseline] = useState<FormValues>(incoming);
  const [latest, setLatest] = useState<FormValues>(incoming);
  const [values, setValues] = useState<FormValues>(incoming);

  // React Query can first return stale cached settings and refresh them after
  // this panel has mounted. Preserve a dirty draft instead of overwriting it.
  if (incomingKey !== seenKey) {
    setSeenKey(incomingKey);
    setLatest(incoming);
    if (
      valuesKey(values) === valuesKey(baseline) ||
      valuesKey(values) === incomingKey
    ) {
      setBaseline(incoming);
      setValues(incoming);
    }
  }
  const [mode, setMode] = useState<Mode>('Light');
  const [previewMode, setPreviewMode] = useState<Mode | 'High Contrast'>(
    'Light'
  );
  const [previewWidth, setPreviewWidth] = useState(1440);
  const mutation = useUpdateSettingsMutation();
  const { appearance } = values;
  const error = validate(values);
  const conflict =
    valuesKey(latest) !== valuesKey(baseline) &&
    valuesKey(latest) !== valuesKey(values);

  function reloadSavedAppearance() {
    setBaseline(latest);
    setValues(latest);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (error || conflict || isRefreshing || mutation.isLoading) return;
    mutation.mutate(
      { Appearance: appearance, LogoURL: values.logo },
      {
        onSuccess(saved) {
          const next = normalizeAppearance(saved.Appearance);
          const savedValues = { appearance: next, logo: saved.LogoURL || '' };
          setBaseline(savedValues);
          setLatest(savedValues);
          setValues(savedValues);
          const injector = angular.element(document).injector();
          injector?.invoke(
            /* @ngInject */ (StateManager: StateManager) =>
              StateManager.updateAppearance(next, saved.LogoURL)
          );
          notifySuccess('Success', 'Appearance settings updated');
        },
      }
    );
  }

  function setAppearance(next: Appearance) {
    setValues((previous) => ({ ...previous, appearance: next }));
  }
  const palette = appearance.Colors[mode];
  return (
    <Widget id="appearance">
      <Widget.Title icon={Palette} title="Appearance" />
      <Widget.Body>
        <p>
          Customize branding, colors, typography and layout for this Portainer
          instance. The preview does not change the live interface until saved.
        </p>
        <form onSubmit={onSubmit} className="appearance-settings">
          <fieldset disabled={mutation.isLoading}>
            <legend>Branding</legend>
            <label>
              Brand name{' '}
              <input
                className="form-control"
                value={appearance.Branding.Name}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Branding: { ...appearance.Branding, Name: e.target.value },
                  })
                }
                maxLength={160}
              />
            </label>
            <label>
              Logo URL{' '}
              <input
                className="form-control"
                value={values.logo}
                onChange={(e) => setValues({ ...values, logo: e.target.value })}
                placeholder="https://example.com/logo.png"
              />
            </label>
            <span className="text-muted small">
              For best results use a logo about 155 × 55 px. Leave empty for the
              Portainer logo.
            </span>
            <label>
              Favicon URL{' '}
              <input
                className="form-control"
                value={appearance.Branding.FaviconURL}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Branding: {
                      ...appearance.Branding,
                      FaviconURL: e.target.value,
                    },
                  })
                }
                placeholder="/favicon.png"
              />
            </label>
          </fieldset>
          <fieldset disabled={mutation.isLoading}>
            <legend>Colors</legend>
            <label>
              Theme to edit{' '}
              <select
                className="form-control"
                value={mode}
                onChange={(e) => {
                  setMode(e.target.value as Mode);
                  setPreviewMode(e.target.value as Mode);
                }}
              >
                <option>Light</option>
                <option>Dark</option>
              </select>
            </label>
            <p className="text-muted small">
              Leave a color empty to use the built-in theme. High Contrast
              always uses the built-in palette.
            </p>
            {paletteFields.map(([key, label]) => (
              <label key={key}>
                {label}{' '}
                <input
                  className="form-control"
                  value={palette[key]}
                  placeholder="#RRGGBB"
                  onChange={(e) =>
                    setAppearance({
                      ...appearance,
                      Colors: {
                        ...appearance.Colors,
                        [mode]: { ...palette, [key]: e.target.value },
                      },
                    })
                  }
                />
              </label>
            ))}
          </fieldset>
          <fieldset disabled={mutation.isLoading}>
            <legend>Typography</legend>
            <label>
              Font{' '}
              <select
                className="form-control"
                value={appearance.Typography.FontFamily}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Typography: {
                      ...appearance.Typography,
                      FontFamily: e.target
                        .value as Appearance['Typography']['FontFamily'],
                    },
                  })
                }
              >
                <option value="inter">Inter</option>
                <option value="system">System UI</option>
                <option value="arial">Arial</option>
              </select>
            </label>
            <label>
              Font size{' '}
              <select
                className="form-control"
                value={appearance.Typography.FontScale}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Typography: {
                      ...appearance.Typography,
                      FontScale: Number(
                        e.target.value
                      ) as Appearance['Typography']['FontScale'],
                    },
                  })
                }
              >
                <option value={100}>100%</option>
                <option value={110}>110%</option>
                <option value={120}>120%</option>
              </select>
            </label>
          </fieldset>
          <fieldset disabled={mutation.isLoading}>
            <legend>Layout and theme</legend>
            <label>
              Sidebar width (px){' '}
              <input
                className="form-control"
                type="number"
                min={240}
                max={360}
                step={1}
                value={appearance.Layout.SidebarWidth}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Layout: {
                      ...appearance.Layout,
                      SidebarWidth: Number(e.target.value),
                    },
                  })
                }
              />
            </label>
            <label>
              Default sidebar{' '}
              <select
                className="form-control"
                value={appearance.Layout.SidebarDefault}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Layout: {
                      ...appearance.Layout,
                      SidebarDefault: e.target
                        .value as Appearance['Layout']['SidebarDefault'],
                    },
                  })
                }
              >
                <option value="open">Open</option>
                <option value="closed">Collapsed</option>
              </select>
            </label>
            <label>
              Density{' '}
              <select
                className="form-control"
                value={appearance.Layout.Density}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    Layout: {
                      ...appearance.Layout,
                      Density: e.target
                        .value as Appearance['Layout']['Density'],
                    },
                  })
                }
              >
                <option value="standard">Standard</option>
                <option value="compact">Compact</option>
              </select>
            </label>
            <label>
              Default theme{' '}
              <select
                className="form-control"
                value={appearance.DefaultTheme}
                onChange={(e) =>
                  setAppearance({
                    ...appearance,
                    DefaultTheme: e.target.value as Appearance['DefaultTheme'],
                  })
                }
              >
                <option value="auto">System</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
                <option value="highcontrast">High Contrast</option>
              </select>
            </label>
            <p className="text-muted small">
              A user’s saved theme and sidebar choice take precedence over these
              defaults.
            </p>
          </fieldset>
          <fieldset disabled={mutation.isLoading}>
            <legend>Preview</legend>
            <label>
              Preview theme{' '}
              <select
                className="form-control"
                value={previewMode}
                onChange={(e) =>
                  setPreviewMode(e.target.value as typeof previewMode)
                }
              >
                <option>Light</option>
                <option>Dark</option>
                <option>High Contrast</option>
              </select>
            </label>
            <label>
              Preview width{' '}
              <select
                className="form-control"
                value={previewWidth}
                onChange={(e) => setPreviewWidth(Number(e.target.value))}
              >
                <option value={390}>Mobile · 390 px</option>
                <option value={768}>Tablet · 768 px</option>
                <option value={1440}>Desktop · 1440 px</option>
              </select>
            </label>
          </fieldset>
          <div className="appearance-preview-viewport">
            <AppearancePreview
              appearance={appearance}
              logo={values.logo}
              mode={previewMode}
              width={previewWidth}
            />
          </div>
          {error && (
            <p role="alert" className="text-danger">
              {error}
            </p>
          )}
          {conflict && (
            <p role="alert" className="text-danger">
              Appearance settings changed while you were editing. Reload the
              saved appearance before saving to avoid overwriting those changes.
            </p>
          )}
          {isRefreshing && (
            <p className="text-muted">Refreshing saved appearance...</p>
          )}
          <div className="flex flex-wrap gap-2">
            <LoadingButton
              data-cy="appearance-save"
              isLoading={mutation.isLoading}
              loadingText="Saving..."
              disabled={!!error || conflict || isRefreshing}
            >
              Save appearance
            </LoadingButton>
            <Button
              data-cy="appearance-cancel"
              disabled={mutation.isLoading}
              color="secondary"
              onClick={reloadSavedAppearance}
            >
              Cancel
            </Button>
            {conflict && (
              <Button
                data-cy="appearance-reload-saved"
                disabled={mutation.isLoading || isRefreshing}
                color="secondary"
                onClick={reloadSavedAppearance}
              >
                Reload saved appearance
              </Button>
            )}
            <Button
              data-cy="appearance-reset"
              disabled={mutation.isLoading}
              color="secondary"
              onClick={() =>
                setValues({
                  appearance: normalizeAppearance(defaultAppearance),
                  logo: '',
                })
              }
            >
              Reset to defaults
            </Button>
          </div>
        </form>
      </Widget.Body>
    </Widget>
  );
}

function AppearancePreview({
  appearance,
  logo,
  mode,
  width,
}: {
  appearance: Appearance;
  logo: string;
  mode: Mode | 'High Contrast';
  width: number;
}) {
  const highContrast = mode === 'High Contrast';
  const colors = highContrast
    ? defaultAppearance.Colors.Dark
    : appearance.Colors[mode];
  function valid(value: string, fallback: string) {
    return /^#[\da-f]{6}$/i.test(value) ? value : fallback;
  }
  const page = valid(
    colors.PageBackground,
    mode === 'Light' ? '#fcfbfa' : highContrast ? '#000000' : '#121212'
  );
  const surface = valid(
    colors.SurfaceBackground,
    mode === 'Light' ? '#ffffff' : highContrast ? '#000000' : '#212121'
  );
  const sidebar = valid(
    colors.SidebarBackground,
    highContrast ? '#000000' : '#2e2f33'
  );
  const accent = valid(colors.Accent, mode === 'Light' ? '#2e2f33' : '#f7f6f3');
  const font = {
    inter: 'Inter, sans-serif',
    system: 'system-ui, sans-serif',
    arial: 'Arial, sans-serif',
  }[appearance.Typography.FontFamily];
  const sidebarOpen =
    width >= 992 && appearance.Layout.SidebarDefault === 'open';
  return (
    <section
      aria-label={`${mode} appearance preview`}
      className="appearance-preview"
      style={{
        width,
        background: page,
        color: contrastText(page),
        fontFamily: font,
        fontSize: `${(14 * appearance.Typography.FontScale) / 100}px`,
      }}
    >
      <aside
        style={{
          background: sidebar,
          color: contrastText(sidebar),
          width: `${sidebarOpen ? Math.max(80, Math.min(140, appearance.Layout.SidebarWidth / 2.5)) : 55}px`,
        }}
      >
        {logo && (
          <img
            key={logo}
            src={logo}
            alt="Logo preview"
            onError={(e) => {
              e.currentTarget.classList.add('hidden');
            }}
          />
        )}
        {sidebarOpen ? (
          <>
            <strong>{appearance.Branding.Name || 'Portainer'}</strong>
            <p>Home</p>
            <p>Environments</p>
            <small>Powered by Portainer</small>
          </>
        ) : (
          <>
            <strong aria-label={appearance.Branding.Name || 'Portainer'}>
              P
            </strong>
            <p>⌂</p>
          </>
        )}
      </aside>
      <div className="appearance-preview-content">
        <h4>Preview · {mode}</h4>
        <article
          style={{
            background: surface,
            color: contrastText(surface),
            padding: appearance.Layout.Density === 'compact' ? '8px' : '12px',
          }}
        >
          <strong>Application</strong>
          <p>Example content</p>
          <label>
            Example field <input value="Value" readOnly />
          </label>
          <table>
            <tbody>
              <tr>
                <td>Container</td>
                <td>Running</td>
              </tr>
            </tbody>
          </table>
          <button
            type="button"
            style={{
              background: accent,
              color: contrastText(accent),
              padding: '6px 12px',
              borderRadius: '4px',
            }}
          >
            Primary action
          </button>
        </article>
      </div>
    </section>
  );
}
