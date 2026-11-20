# Appearance review fixes: localhost deployment

Deployment on 2026-11-18 targeted only the existing Compose service
`portainer-custom/portainer`. No other container was recreated.

- New image: `portainer-custom:2.45.3-appearance-reviewfix-20261118`
- New image ID: `sha256:b9c5ae53ed74d9a4aa48c5dbbd6ae595dc5e5d53669cb675003e323d2c4df75d`
- Replacement container: `4163934502c281388d02e593e1780be9ab2e1999c06060f70c242857dda611e1`
- Previous image: `portainer-custom:2.45.3-volume-tooltip`
- Previous image ID: `sha256:f75ef9345f0de1fb1a454fa26f1d514ca365f53e4f4b6066a2da38a657571256`
- Verified `/data` backup volume: `portainer_reviewfix_backup_20261118_1315`

The original Portainer container was stopped before its data volume was copied
with `cp -a` into the backup volume. `diff -qr` against the stopped source
volume returned success. The backup is retained; it contains sensitive Portainer
data and must be protected like the original volume.

After deployment, the replacement was running with the original data and stack
configuration volumes, command, restart policy, network and loopback port
bindings (`127.0.0.1:9000` and `127.0.0.1:9445`). The HTTPS root,
`/api/status` and `/api/settings/public` returned HTTP 200; the latter returned
Appearance settings. Independent smoke checks also fetched the HTML and all
five referenced frontend assets. The existing `.env` image assignment was
updated to the new tag without changing the rest of the file.

The image was rebuilt from the working tree using
`deploy/standalone/Dockerfile`. Go race tests, focused frontend tests,
TypeScript, lint and isolated-image HTTP smoke checks passed before replacement.
Browser E2E against the live application was not performed.

## Rollback

The previous image remains available locally. To revert the running service
without touching the data volume, from the repository root in PowerShell:

```powershell
$env:PORTAINER_IMAGE = 'portainer-custom:2.45.3-volume-tooltip'
docker compose -p portainer-custom -f deploy/standalone/compose.yaml -f deploy/standalone/compose.desktop.yaml up -d --no-deps --no-build portainer
Remove-Item Env:PORTAINER_IMAGE
```

Then change only `PORTAINER_IMAGE` in `deploy/standalone/.env` back to the old
image tag so a later Compose operation does not restore the newer image. Verify
the HTTPS status endpoint and mounts after rollback. The retained backup is a
point-in-time copy, not a live replica; restoring it discards any later changes
to `/data` and should be done only when recovery is actually needed.
