# Security

## Reporting

If you believe you have found a security issue, please report it privately.
Do not open a public GitHub issue for sensitive reports.

## Secrets

- Do not commit `.env` files, API keys, tokens, or any auth profile JSON.
- The repo is set up to ignore common local secret locations; double check before pushing.
- Realtime providers (e.g. ElevenLabs) require server-side auth. Never ship long-lived keys to the browser.

