# Admin Analytics Dashboard

> Admin-only analytics page showing users, tunnels, subscriptions, and trends.

---

## Overview

The Admin Dashboard (`/dashboard/admin`) displays aggregate statistics and recent activity for tunr.sh:

- **User stats**: Total, free, and pro users
- **Tunnel stats**: Total, active, and tunnels created today
- **Subscriptions**: Active paid subscriptions
- **7-day trends**: Signups and tunnels created (bar charts)
- **Recent activity**: Last 10 signups and last 10 tunnels

Only users whose email is listed in `ADMIN_EMAILS` can access this page. Non-admin users who visit `/dashboard/admin` are redirected to `/dashboard`.

---

## Environment Variable

| Variable | Description |
|----------|-------------|
| `ADMIN_EMAILS` | Comma-separated list of admin email addresses (case-insensitive) |

**Example:**
```
ADMIN_EMAILS=ahmet@tunr.sh,admin@tunr.sh
```

---

## Server Setup

1. **Add `ADMIN_EMAILS` to `.env`** (in the Next.js dashboard directory, e.g. `/home/tunr/tunr/landing/app`):

   ```bash
   echo "ADMIN_EMAILS=ahmet@tunr.sh" >> .env
   ```

   For multiple admins:
   ```bash
   ADMIN_EMAILS=ahmet@tunr.sh,admin@tunr.sh
   ```

2. **Restart the dashboard**:

   ```bash
   pm2 restart tunr-dashboard
   # or, if using systemd:
   sudo systemctl restart tunr-dashboard
   ```

3. **Verify** that the dashboard process has picked up the new env (check `pm2 env tunr-dashboard` or equivalent).

---

## Testing

1. **Log in** to the app at `https://app.tunr.sh` with an email in `ADMIN_EMAILS`.

2. **Confirm Admin link** in the sidebar (Admin nav item appears only for admin users).

3. **Open Admin page** (`/dashboard/admin`):
   - Stats cards load (users, tunnels, subscriptions)
   - 7-day trend bar charts render
   - Recent signups and recent tunnels tables show data

4. **Test non-admin access**:
   - Log in with a different (non-admin) email
   - Navigate directly to `/dashboard/admin` → should redirect to `/dashboard`
   - Sidebar should not show the Admin link
