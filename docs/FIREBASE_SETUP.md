# Firebase Google Sign-In Setup

The "Sign in with Google" button on the login page uses Firebase Authentication. Follow this guide to configure Firebase and environment variables for local and production.

---

## Quick integration (if Firebase app already exists)

If your Firebase project and web app are already created, you only need to define environment variables. The code integration is already in place.

### 1) Values you need from Firebase

| Source | Variable |
|--------|----------|
| **Project settings → General → Your apps → Web app → firebaseConfig** | |
| `apiKey` | `NEXT_PUBLIC_FIREBASE_API_KEY` |
| `authDomain` | `NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN` |
| `projectId` | `NEXT_PUBLIC_FIREBASE_PROJECT_ID` |
| `storageBucket` | `NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET` |
| `messagingSenderId` | `NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID` |
| `appId` | `NEXT_PUBLIC_FIREBASE_APP_ID` |
| **Project settings → Service accounts → Generate new private key** | |
| Full downloaded JSON content (single line) | `FIREBASE_SERVICE_ACCOUNT_JSON` |

### 2) Local development

```bash
cd /path/to/tunr/landing/app
cp .env.local.example .env.local
```

Fill Firebase placeholders in `.env.local`, then run:

```bash
npm run dev
```

Google sign-in should work on localhost once `localhost` is present in Authorized Domains.

### 3) Server (Hetzner / `/opt/tunr`)

Add the same variables to your server env file (for example `/opt/tunr/.env` or `/opt/tunr/src/landing/app/.env`). Keep `FIREBASE_SERVICE_ACCOUNT_JSON` as single-line JSON.

```bash
cd /opt/tunr/src/landing/app
npm run build
systemctl restart tunr-dashboard
```

### 4) Required authorized domains

In Firebase Console → **Authentication** → **Settings** → **Authorized domains**, add:

- `localhost` (local test)
- `app.tunr.sh` (production dashboard)

---

## 1. Create a Firebase project

1. Open [Firebase Console](https://console.firebase.google.com/).
2. Create a new project (or select an existing one).
3. Project name example: `tunr-app`.
4. Google Analytics is optional.

---

## 2. Enable Google Sign-In

1. **Build** → **Authentication** → **Get started**.
2. Open **Sign-in method** tab.
3. Enable **Google**.
4. Select a project support email and save.

---

## 3. Add authorized domains

Google sign-in only works on authorized domains:

1. Go to **Authentication** → **Settings** → **Authorized domains**.
2. Ensure `localhost` exists.
3. Add `app.tunr.sh`.
4. Add `tunr.sh` if your flow redirects from landing.

---

## 4. Configure Web App (client)

1. Open **Project settings** → **General**.
2. Under **Your apps**, click the **Web (`</>`)** icon.
3. App nickname example: `tunr-dashboard`.
4. Firebase Hosting is optional.
5. Copy `firebaseConfig` values:

```javascript
const firebaseConfig = {
  apiKey: "AIza...",
  authDomain: "tunr-app.firebaseapp.com",
  projectId: "tunr-app",
  storageBucket: "tunr-app.appspot.com",
  messagingSenderId: "123456789",
  appId: "1:123456789:web:abc..."
};
```

Map to env vars:

| Env var | Firebase value |
|---------|----------------|
| `NEXT_PUBLIC_FIREBASE_API_KEY` | `apiKey` |
| `NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN` | `authDomain` |
| `NEXT_PUBLIC_FIREBASE_PROJECT_ID` | `projectId` |
| `NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET` | `storageBucket` |
| `NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID` | `messagingSenderId` |
| `NEXT_PUBLIC_FIREBASE_APP_ID` | `appId` |

---

## 5. Service account (server key)

`/api/auth/firebase-token` verifies Firebase ID tokens server-side using Firebase Admin SDK. You need a service account JSON key.

1. **Project settings** → **Service accounts**.
2. Click **Generate new private key**.
3. Keep full JSON content securely.

Do not expose this JSON on client side or in public repositories.

### Recommended server options

- **Option A (recommended):** `FIREBASE_SERVICE_ACCOUNT_PATH=/opt/tunr/firebase-key.json`
- **Option B:** `FIREBASE_SERVICE_ACCOUNT_JSON=<single-line-json>`

If you see `"Failed to parse private key"`, switch to Option A.

Example JSON (truncated):

```json
{"type":"service_account","project_id":"tunr-app","private_key_id":"...","private_key":"-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n","client_email":"firebase-adminsdk-xxxxx@tunr-app.iam.gserviceaccount.com"}
```

---

## 6. Where to define variables

### Local (`landing/app/.env.local`)

```bash
# Firebase client config
NEXT_PUBLIC_FIREBASE_API_KEY=AIza...
NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN=tunr-app.firebaseapp.com
NEXT_PUBLIC_FIREBASE_PROJECT_ID=tunr-app
NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET=tunr-app.appspot.com
NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID=123456789
NEXT_PUBLIC_FIREBASE_APP_ID=1:123456789:web:abc...

# Firebase Admin (single line)
FIREBASE_SERVICE_ACCOUNT_JSON={"type":"service_account","project_id":"tunr-app",...}
```

### Hetzner server

Typical env file locations:

- `/opt/tunr/.env`
- `/opt/tunr/src/landing/app/.env`

After changes:

```bash
cd /opt/tunr/src/landing/app
npm run build
systemctl restart tunr-dashboard
```

### Netlify

In **Site settings → Environment variables**:

- Add all `NEXT_PUBLIC_FIREBASE_*` variables
- Add `FIREBASE_SERVICE_ACCOUNT_JSON` as single-line JSON

---

## 7. Troubleshooting `500` on `/api/auth/firebase-token`

Inspect response `code` in browser network tab:

| code | Meaning | Fix |
|------|---------|-----|
| `FIREBASE_NOT_CONFIGURED` | Missing service account or private key parse failure | Use `FIREBASE_SERVICE_ACCOUNT_PATH` or valid single-line JSON |
| `JWT_NOT_CONFIGURED` | Missing JWT secret | Set `JWT_SECRET` with 32+ chars (`openssl rand -hex 32`) |
| `DATABASE_UNREACHABLE` | Database not reachable | Verify `DATABASE_URL` and network access |
| `SERVER_ERROR` | Other backend failure | Check logs (`journalctl -u tunr-dashboard -f` or Netlify function logs) |

After env changes, always rebuild and restart.

---

## 8. Checklist

- [ ] Firebase project created
- [ ] Google sign-in enabled
- [ ] Authorized domains include `app.tunr.sh` and `localhost`
- [ ] Web app config copied
- [ ] `NEXT_PUBLIC_FIREBASE_*` variables set
- [ ] Service account key generated
- [ ] `FIREBASE_SERVICE_ACCOUNT_JSON` or `FIREBASE_SERVICE_ACCOUNT_PATH` configured
- [ ] Server/Netlify rebuilt and restarted

After this setup, Google sign-in should work on the login page.
