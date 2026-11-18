# Appearance validation receipt

Validation for the Portainer CE Appearance implementation and its local Docker
deployment. No public or production service was changed.

## Automated checks

| Check | Result |
| --- | --- |
| Go auth, authlog, and useractivity tests with `-race` | Passed using `golang:1.26.10-bookworm`, including 1,000 concurrent rejected requests sharing a bounded sampler and persistence of the suppressed count on the next sample. |
| Appearance React and runtime tests | Passed: 13 tests across two Appearance files, covering legacy defaults, branding/favicon/font/layout application and reset, light/dark/high-contrast colors, extreme surface/sidebar colors, preview isolation, save/cancel/reset, pristine settings refetch, dirty-draft conflicts, form validation, and sidebar preference precedence. |
| TypeScript | `pnpm exec tsc --noEmit` passed. |
| Changed-file ESLint | Passed for Appearance UI/runtime, settings integration, sidebar and legacy Angular integration, test files, and Storybook stories. |
| Production frontend bundle | `pnpm build` passed; webpack compiled successfully in about 58 seconds. `dist/public/index.html` and hashed main assets were emitted; the built CSS contains the Appearance page, contrast accent, and preview rules. Storybook fixtures remain source-only. |

The Go tests cover old settings receiving defaults, public settings exposing only
the intended Appearance fields, PUT persistence, preservation when existing
settings requests omit Appearance, rejection of invalid Appearance without
partially saving a simultaneous logo update, and persistence after closing and
reopening the isolated BoltDB store. A bouncer test denies admin-protected
settings routes and permits the public settings route. It checks route policy
wiring with a test bouncer; it does not exercise real JWT issuance or a live
authenticated admin/non-admin session. The added auth tests prove that denied
requests are explicitly marked and sampled globally, successful and ordinary
failed authentication events remain unsampled, suppressed counts are bounded
and reported on the next persisted sample, and concurrent sampling is race-free.

Vitest emitted upstream notices for the Vite tsconfig-paths plugin and stale
Browserslist data. The production build also warned that Node 24.21.0 is outside
the repository's declared Node `^22.22.1` range, `.env` is absent, Browserslist
data is stale, and an upstream Babel API is deprecated. These warnings did not
fail the checks.

## Browser validation

The Appearance panel was rendered in Storybook at `http://localhost:6011` in a
Chrome extension browser tab. Storybook/MSW supplied the UI context; no Portainer
API or persistent settings were involved.

- The default story exposed Branding, Colors, Typography, Layout and theme,
  Preview, Save, Cancel, and Reset controls.
- The preview-width selector rendered sections at 390, 768, and 1440 CSS px
  (measured widths 390.000, 767.986, and 1440.000 px). The preview wrapper had
  `overflow-x: auto`; at the observed large host viewport the page
  `scrollWidth` matched its `clientWidth` for all three widths.
- The mobile and tablet previews collapsed the sidebar to 55 px; the desktop
  preview opened it to 140 px. Compact used 8 px card padding. Arial at 120%
  rendered as `Arial, sans-serif` and 16.8 px in the preview.
- Custom light values (`Accent #FFFFFF`, `PageBackground #F7F8FC`, black
  surface, white sidebar) rendered black text on the white button and sidebar,
  black page text, and white text on the black card. An all-black Dark palette
  rendered white foregrounds. High Contrast used its built-in black/white
  surfaces despite the custom draft palette.
- Editing the brand name updated the preview and retained the “Powered by
  Portainer” line. The long-brand story rendered the complete name with
  `overflow-wrap: anywhere`. The extreme-color story also rendered readable
  foregrounds on its light page, dark surface, and white accent button.
- Cancel restored the stored default values after editing. Reset cleared the
  draft brand and a local missing-logo URL and restored the default form values;
  Save remained enabled. Save was not clicked because Storybook has no mocked
  `PUT /api/settings` handler. The React component test covers the mutation
  payload, but this browser check does not verify API persistence.

The browser's default measured viewport after reset was 2262 × 1500 px. The
viewport override did not report the requested 390/768 CSS viewport dimensions,
so this check does not establish full-application behavior on 390/768/1440 px
device viewports. It checks the Appearance preview's width selector and DOM
styles at those widths. Screenshot capture timed out at the browser's
`Page.captureScreenshot` command; no screenshot artifact was produced. The
Storybook server was stopped after the check.

## Review regression checks

Targeted ESLint for the changed Appearance panel/runtime, sidebar components and
store, React Select, Settings integration, and regression tests passed after the
conflict reload control was given its required automation identifier. The first
TypeScript check found this missing `data-cy` prop; it was fixed, and the
subsequent `pnpm exec tsc --noEmit` passed. Appearance Vitest passed 13/13 and
the focused Go packages passed with `-race` after updating the authentication
CSV header expectation for its two rate-limit sampling columns.

## Latest image smoke check

Image `portainer-custom:2.45.3-appearance-reviewfix-20261118` at digest
`sha256:b9c5ae53ed74d9a4aa48c5dbbd6ae595dc5e5d53669cb675003e323d2c4df75d`
was started in a temporary container with its own `/data` volume and ephemeral
localhost-only ports. `/api/status`, `/api/settings/public`, the application
HTML, and all five referenced hashed JavaScript/CSS assets returned HTTP 200.
The public Appearance response contained the expected legacy defaults; the main
JavaScript and CSS assets contained Appearance implementation markers.
The temporary container and volume were removed after the check. This smoke did
not create an admin, mutate application settings, or exercise authenticated API
updates.

## Local container deployment check

The existing localhost Portainer container was recreated from the same image
digest after creating a data backup volume. The new container uses image
`portainer-custom:2.45.3-appearance-reviewfix-20261118` at digest
`sha256:b9c5ae53ed74d9a4aa48c5dbbd6ae595dc5e5d53669cb675003e323d2c4df75d`.
A separate local deployment record contains the machine-specific details.
The Compose `PORTAINER_IMAGE` pin matches the running image. Its localhost port
bindings stayed at 9000 and 9445. Read-only inspection confirmed that the prior
`/data`, stack-config, Docker socket, and read-only `/host` mounts remain
attached. No settings or user accounts were changed.

After the recreate, `/api/status` reported 2.45.3, `/api/settings/public`
returned the Appearance defaults, the UI HTML returned HTTP 200, and each of
the five referenced hashed JS/CSS assets returned HTTP 200. The main JS and CSS
contained Appearance implementation markers. This validates the live local
container and served assets; it does not exercise an authenticated settings
write.

## Limits

This receipt does not claim a full repository test run, production deployment,
or live Portainer API acceptance. Runtime behavior is covered by focused unit
tests and the isolated browser check, not a production environment.
