# GoPulse Admin Frontend

Independent Vue application mounted at `/admin/` for authenticated super
administrators. The default page is the management dashboard. Current routes are
defined in [src/router/index.ts](src/router/index.ts):

| Path | Page |
| --- | --- |
| `/admin/` | Dashboard |
| `/admin/metrics` | Metrics |
| `/admin/logs` | Logs |
| `/admin/events` | Events |
| `/admin/alerts` | Alerts |
| `/admin/plugins` | Six-plugin management |
| `/admin/users` | User roles |
| `/admin/audit` | Audit records |

Pages use the existing strict Backend DTOs and share the user application's
identity system. Product-wide verification and limitations are recorded in
[capability status](../dev/status/capability-status.md); usage is described in the
[product manual](../使用手册.md).

The supported product entry is the published `frontend` Compose origin.
`admin-frontend` listens internally on 8080, joins only `edge`, and publishes no
host port. Nginx preserves the incoming `/admin/` prefix and serves assets below
`/admin/assets/`; the edge redirects historical `/admin/observability/*` links.
Both containers run as numeric non-root users with read-only filesystems and
`/tmp` tmpfs. Authentication uses the same HttpOnly cookie as the user application.
A 403 unmounts management data and erases candidate secrets before navigating to
`/posts`; it does not log the social session out.

Local checks, from `admin-frontend/`:

```bash
npm ci
npm test
npm run typecheck
npm run build
```

For UI development, start `npm run dev` in both applications. Use only the user
Vite origin on port 5173; it proxies `/admin` to the admin Vite server on 5174.

For the source-based browser gate, use `make e2e SCOPE=observe` from the repository
root. It starts isolated test dependencies, both Vite servers, the source Backend,
Router, Marshaller, and the fixed Linux Monitor image; it then checks same-origin
cookies, administrator data, ordinary-user denial, role demotion, and current
metrics/logs/events/plugin state. It stops only processes and resources owned by
that test project. `make test MODULE=admin-frontend` remains the focused Vitest
entry.
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
