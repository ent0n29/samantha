import json
import os
import pathlib
import base64
from datetime import datetime, timezone


def _read_json(path: pathlib.Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def _write_json(path: pathlib.Path, obj: dict, *, mode: int | None = None) -> None:
    path.write_text(json.dumps(obj, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if mode is not None:
        os.chmod(path, mode)

def _jwt_exp_ms(jwt: str) -> int:
    """Extract the exp claim (epoch seconds) from a JWT without verification."""
    try:
        parts = jwt.split(".")
        if len(parts) < 2:
            return 0
        payload = parts[1]
        payload += "=" * ((4 - (len(payload) % 4)) % 4)
        data = base64.urlsafe_b64decode(payload.encode("utf-8"))
        obj = json.loads(data.decode("utf-8"))
        exp = obj.get("exp")
        if isinstance(exp, (int, float)):
            return int(exp) * 1000
        if isinstance(exp, str) and exp.isdigit():
            return int(exp) * 1000
    except Exception:
        return 0
    return 0


def main() -> int:
    home = pathlib.Path.home()
    codex_auth = home / ".codex" / "auth.json"
    if not codex_auth.exists():
        # Not a hard error: the project can still run with mock replies.
        return 0

    codex = _read_json(codex_auth)
    tokens = codex.get("tokens") or {}
    access = tokens.get("access_token")
    refresh = tokens.get("refresh_token")
    account_id = tokens.get("account_id")
    if not (isinstance(access, str) and access.strip()):
        return 0
    exp_ms = _jwt_exp_ms(access.strip())

    openclaw_dir = home / ".openclaw"
    agent_dir = openclaw_dir / "agents" / "main" / "agent"
    sessions_dir = openclaw_dir / "agents" / "main" / "sessions"
    agent_dir.mkdir(parents=True, exist_ok=True)
    sessions_dir.mkdir(parents=True, exist_ok=True)
    (openclaw_dir / "credentials").mkdir(parents=True, exist_ok=True)

    # Lock down state dir permissions; OpenClaw stores secrets in auth-profiles.json.
    try:
        os.chmod(openclaw_dir, 0o700)
    except OSError:
        pass

    auth_path = agent_dir / "auth-profiles.json"
    store = {"version": 1, "profiles": {}}
    if auth_path.exists():
        try:
            store = _read_json(auth_path)
        except Exception:
            store = {"version": 1, "profiles": {}}

    profiles = store.get("profiles")
    if not isinstance(profiles, dict):
        profiles = {}
        store["profiles"] = profiles

    now = datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    key = "openai-codex:default"
    existing = profiles.get(key)
    if not isinstance(existing, dict):
        existing = {}

    # Only seed refresh tokens if missing; OpenClaw may rotate refresh tokens after use.
    if not (isinstance(existing.get("refresh"), str) and existing.get("refresh").strip()):
        if not (isinstance(refresh, str) and refresh.strip()):
            return 0
        existing["refresh"] = refresh.strip()

    # Access tokens are short-lived; it's safe to refresh them from Codex.
    existing["access"] = access.strip()

    if exp_ms and (not isinstance(existing.get("expires"), int) or int(existing.get("expires") or 0) <= 0):
        # Avoid forcing an OAuth refresh on every run; let OpenClaw refresh when near expiry.
        existing["expires"] = exp_ms

    if not (isinstance(existing.get("accountId"), str) and existing.get("accountId").strip()):
        if not (isinstance(account_id, str) and account_id.strip()):
            return 0
        existing["accountId"] = account_id.strip()

    existing["type"] = "oauth"
    existing["provider"] = "openai-codex"
    existing["syncedFrom"] = "codex"
    existing["syncedAt"] = now
    profiles[key] = existing

    store["version"] = int(store.get("version") or 1)
    _write_json(auth_path, store, mode=0o600)

    # Ensure OpenClaw defaults to the Codex-subscription provider model.
    cfg_path = openclaw_dir / "openclaw.json"
    cfg = {}
    if cfg_path.exists():
        try:
            cfg = _read_json(cfg_path)
        except Exception:
            cfg = {}

    agents = cfg.setdefault("agents", {})
    defaults = agents.setdefault("defaults", {})
    model_cfg = defaults.setdefault("model", {})
    model_cfg["primary"] = "openai-codex/gpt-5.3-codex"
    models_cfg = defaults.setdefault("models", {})
    if isinstance(models_cfg, dict):
        models_cfg.setdefault("openai-codex/gpt-5.3-codex", {})

    # If we created the file, lock it down; otherwise respect existing perms.
    _write_json(cfg_path, cfg, mode=(0o600 if not cfg_path.exists() else None))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
