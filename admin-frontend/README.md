# GoPulse Admin Frontend

Independent Vue application mounted at `/admin/`. Metrics, Logs, Events and the
six-plugin management UI use the existing strict Backend DTOs. `/admin/` redirects
to Metrics after session bootstrap; dashboards, alerts, users and audit pages are
not implemented in this batch.

The supported product entry is the published `frontend` Compose origin.
`admin-frontend` listens internally on 8080, joins only `edge`, and publishes no
host port. Nginx preserves the incoming `/admin/` prefix and serves assets below
`/admin/assets/`; the edge redirects historical `/admin/observability/*` links.
Both containers run as numeric non-root users with read-only filesystems and
`/tmp` tmpfs. Authentication uses the same HttpOnly cookie as the user application.
A 403 unmounts management data and erases candidate secrets before navigating to
`/posts`; it does not log the social session out.

Local checks:

```bash
npm ci
npm test
npm run typecheck
npm run build
```

For UI development, start `npm run dev` in both applications. Use only the user
Vite origin on port 5173; it proxies `/admin` to the admin Vite server on 5174.
Container acceptance remains authoritative:

```bash
bash scripts/verify-admin-frontend.sh --self-test
bash scripts/verify-admin-frontend.sh --existing-management
```

The real gate builds current images and uses a unique owned Compose project,
real Backend sessions/data and a browser using only the published edge origin.
It removes its containers, networks, volumes and uniquely tagged images on exit.
Evidence is retained under the printed `.run/gopulse-p1401-<token>/` directory;
this prefix reuses the established ownership-safe acceptance helper.
