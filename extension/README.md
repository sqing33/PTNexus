# PT Nexus Cookie Sync Extension

This folder contains a Chrome/Edge MV3 extension that syncs browser cookies to the `server` backend.

## Backend APIs used

- `POST /api/auth/login`
- `GET /api/sites/cookie_sync_targets`
- `POST /api/sites/cookie_sync_batch`

## Install (unpacked)

1. Open Chrome/Edge extension page.
2. Enable developer mode.
3. Click `Load unpacked`.
4. Select this `extension/` folder.

## Usage

1. Fill API base URL:
   - updater direct access on LAN/local machine: `http://<your-host>:5274`
   - HTTPS reverse proxy deployment: `https://<your-domain>`
2. Fill PT Nexus username and password.
3. Click `Login Test`.
4. Click `Sync Cookies`.
5. Grant requested host permissions when prompted.

## Notes

- The extension reads cookies only from the current browser profile.
- Cookie values are not printed in logs/results, only sync status is shown.
- Password is stored only if `Remember password locally` is checked.
- If `ERR_SSL_PROTOCOL_ERROR` appears, your updater is likely HTTP-only. The extension now auto-falls back to `http://` and saves the corrected API base URL.
