# Landing page: Login vs Dashboard CTA

When the user is logged in to the app (app.tunr.sh), the static landing (tunr.sh) can show "Dashboard" instead of "Login" by reading the `tunr_logged_in` cookie set by the app.

## Cookie

- The app sets a non-httpOnly cookie `tunr_logged_in=1` with `domain=.tunr.sh` when the user logs in (magic link or Google).
- On logout, the app clears this cookie.
- So the static site on tunr.sh can read it via `document.cookie`.

## Static HTML snippet

Add an element for the auth CTA and a script that toggles it:

```html
<!-- Auth CTA: show Login or Dashboard based on cookie -->
<a id="auth-cta" href="https://app.tunr.sh/login">Login</a>

<script>
(function() {
  var el = document.getElementById('auth-cta');
  if (!el) return;
  if (document.cookie.indexOf('tunr_logged_in=1') !== -1) {
    el.href = 'https://app.tunr.sh/dashboard';
    el.textContent = 'Dashboard';
  }
})();
</script>
```

Or use a data attribute and two links, show one:

```html
<span id="auth-cta-login"><a href="https://app.tunr.sh/login">Login</a></span>
<span id="auth-cta-dashboard" style="display:none"><a href="https://app.tunr.sh/dashboard">Dashboard</a></span>
<script>
(function() {
  if (document.cookie.indexOf('tunr_logged_in=1') !== -1) {
    var login = document.getElementById('auth-cta-login');
    var dash = document.getElementById('auth-cta-dashboard');
    if (login) login.style.display = 'none';
    if (dash) dash.style.display = '';
  }
})();
</script>
```

Place this in the static site served at tunr.sh (e.g. in the header or nav where Login/Dashboard appears).

## Use Scenarios page

A dedicated **Use scenarios** page is at `landing/use-scenarios.html`. Deploy it with the rest of the static landing (e.g. copy `landing/*` to `/var/www/tunr/`). In the main landing nav, add a link next to "Features":

- **Features** → `/#features` or `/#features`
- **Use scenarios** → `/use-scenarios.html`
