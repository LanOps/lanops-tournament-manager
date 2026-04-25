# Changelog

## [0.1.1] - 2026-04-25

### Fixed

- **XSS protection**: All Go templates now use `html/template` instead of `text/template`, which auto-escapes user-controlled content in HTML output. The `dev/login` handler was also converted from raw string substitution to `html/template`.
- **Template render errors**: Template execution errors no longer silently produce a truncated 200 response; the server now returns HTTP 500 on failure.
- **Security headers**: Every HTTP response now includes `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy`, and `Permissions-Policy`. HSTS is added when `SECURE_COOKIES=true`.
- **CSP inline scripts**: Service worker registration and admin form scripts moved to external files so the strict CSP applies cleanly. CDN script tags for htmx and htmx-ext-sse now carry SRI integrity hashes.
- **CODEOWNERS**: `.github/CODEOWNERS` now protects the entire `.github/` directory (previously only `workflows/`), preventing a compromised contributor from replacing the CODEOWNERS file itself.
- **Startup warning**: `SECURE_COOKIES=false` now logs a warning at startup alongside the `DEV_LOGIN` warning.

## [0.1.0]

Initial release.

- Single and double elimination bracket generation
- Discord OAuth2 login
- Real-time bracket updates via Server-Sent Events
- Discord bot with slash commands for tournament management
- Admin panel for bracket generation and result overrides
- CSRF protection on all forms
- Integration tests with isolated Postgres schemas per test
