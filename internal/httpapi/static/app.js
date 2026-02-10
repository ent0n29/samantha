const MIC_TARGET_SAMPLE_RATE = 16000;
const MIC_PROCESSOR_BUFFER = 2048;
const BARGE_PREROLL_MS = 650;
const BARGE_PREROLL_MAX_BYTES = Math.floor(MIC_TARGET_SAMPLE_RATE * 2 * (BARGE_PREROLL_MS / 1000));
const BARGE_RMS_THRESHOLD = 0.02;
const BARGE_FRAMES_THRESHOLD = 2;
const BARGE_COOLDOWN_MS = 700;


// Lightweight client VAD for "always-on mic" without buffering silence forever.
// This is intentionally heuristic (no ML): dynamic threshold + impulse/noise rejection.
const VAD_PREROLL_MS = 360;
const VAD_PREROLL_MAX_BYTES = Math.floor(MIC_TARGET_SAMPLE_RATE * 2 * (VAD_PREROLL_MS / 1000));
const VAD_MIN_RMS = 0.008;
const VAD_RELATIVE_THRESHOLD = 2.4;
const VAD_MAX_ZCR = 0.25; // zero-crossing rate (0..1). Higher tends to be noise/clicks.
const VAD_MAX_CREST = 25; // peak/rms. Very high tends to be impulse noise (keyboard clicks).
const VAD_ATTACK_FRAMES = 3; // ~250ms with 4096 @ 48kHz; prevents single-click triggers.
const VAD_RELEASE_FRAMES = 8; // ~650ms hangover before auto-commit.

const state = {
  sessionId: "",
  ws: null,
  connected: false,
  micActive: false,
  seq: 0,
  audioContext: null,
  mediaStream: null,
  mediaSource: null,
  processor: null,
  silentGain: null,
  audioQueue: Promise.resolve(),
  audioByTurn: new Map(),
  audioFormatByTurn: new Map(),
  assistantTextByTurn: new Map(),
  playbackToken: 0,
  currentAudio: null,
  playbackContext: null,
  playbackGain: null,
  playbackNextTime: 0,
  playbackSources: new Set(),
  streamScheduleQueue: Promise.resolve(),
  ignoredTurns: new Set(),

  presence: "disconnected",
  micEnergyTarget: 0,
  speakEnergyTarget: 0,
  energy: 0,

  reconnectTimer: null,
  reconnectBackoffMs: 250,
  lastTurnID: "",

  captionClearTimer: null,

  longPressTimer: null,
  longPressFired: false,

  handsFree: false,
  awakeUntilMs: 0,
  manualArmUntilMs: 0,

  voiceId: "",
  voicesLoaded: false,
  voicesLoading: false,

  bargeInFrames: 0,
  lastBargeSentAtMs: 0,
  bargeInActive: false,
  bargePreroll: [],
  bargePrerollBytes: 0,

  // Simple client-side VAD to trigger server-side commit without requiring a manual stop.
  lastVoiceAtMs: 0,
  sawSpeech: false,
  lastAutoCommitAtMs: 0,
  vadNoiseRMS: 0,
  vadSpeechFrames: 0,
  vadSilenceFrames: 0,
  vadStreaming: false,
  vadPreroll: [],
  vadPrerollBytes: 0,

  vizCtx: null,
  vizCSSW: 0,
  vizCSSH: 0,
  vizDPR: 1,
  vizStartMs: 0,
  vizSeed: Math.random() * 1000,
  vizReducedMotion: false,

  onboardingOpen: false,
  onboardingStatus: null,
  micPermission: "unknown", // unknown|granted|prompt|denied
};

const el = {
  viz: document.getElementById("viz"),
  pulse: document.getElementById("pulse"),
  captions: document.querySelector(".captions"),
  captionPrimary: document.getElementById("captionPrimary"),
  captionSecondary: document.getElementById("captionSecondary"),
  sheet: document.getElementById("sheet"),
  closeSheetBtn: document.getElementById("closeSheetBtn"),
  userId: document.getElementById("userId"),
  persona: document.getElementById("persona"),
  voice: document.getElementById("voice"),
  handsFree: document.getElementById("handsFree"),
  newSessionBtn: document.getElementById("newSessionBtn"),
  previewVoiceBtn: document.getElementById("previewVoiceBtn"),
  stopMicBtn: document.getElementById("stopMicBtn"),
  disconnectBtn: document.getElementById("disconnectBtn"),
  endSessionBtn: document.getElementById("endSessionBtn"),
  debug: document.getElementById("debug"),
  events: document.getElementById("events"),

  onboard: document.getElementById("onboard"),
  onboardClose: document.getElementById("onboardClose"),
  onboardChecks: document.getElementById("onboardChecks"),
  onboardHandsFree: document.getElementById("onboardHandsFree"),
  onboardMicBtn: document.getElementById("onboardMicBtn"),
  onboardHearBtn: document.getElementById("onboardHearBtn"),
  onboardSettingsBtn: document.getElementById("onboardSettingsBtn"),
};

function init() {
  hydratePrefs();
  wireUI();
  wireOnboarding();
  wirePointerParallax();
  initViz();
  startEnergyLoop();

  const onboarded = localStorage.getItem("samantha.onboarded") === "1";
  setPresence("disconnected", "Samantha", onboarded ? "Connecting…" : "Welcome. Hold anywhere for settings.");
  void ensureSessionConnected();
  void initOnboarding();
}

function initViz() {
  if (!el.viz) {
    return;
  }
  if (window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    state.vizReducedMotion = true;
  }

  const ctx = el.viz.getContext("2d", { alpha: true, desynchronized: true });
  if (!ctx) {
    return;
  }
  state.vizCtx = ctx;
  state.vizStartMs = performance.now();
  resizeViz();

  window.addEventListener(
    "resize",
    () => {
      resizeViz();
    },
    { passive: true },
  );
}

function resizeViz() {
  if (!el.viz || !state.vizCtx) {
    return;
  }
  const dpr = Math.min(2, window.devicePixelRatio || 1);
  const cssW = el.viz.clientWidth || window.innerWidth || 1;
  const cssH = el.viz.clientHeight || window.innerHeight || 1;
  const w = Math.max(1, Math.floor(cssW * dpr));
  const h = Math.max(1, Math.floor(cssH * dpr));
  if (el.viz.width !== w || el.viz.height !== h || state.vizCSSW !== cssW || state.vizCSSH !== cssH || state.vizDPR !== dpr) {
    el.viz.width = w;
    el.viz.height = h;
    state.vizCSSW = cssW;
    state.vizCSSH = cssH;
    state.vizDPR = dpr;
    state.vizCtx.setTransform(dpr, 0, 0, dpr, 0, 0);
    state.vizCtx.lineJoin = "round";
    state.vizCtx.lineCap = "round";
  }
}

function wireUI() {
  el.pulse.addEventListener("pointerdown", () => {
    unlockAudio();
    if (state.longPressTimer) {
      clearTimeout(state.longPressTimer);
      state.longPressTimer = null;
    }
    state.longPressFired = false;
    state.longPressTimer = setTimeout(() => {
      state.longPressTimer = null;
      state.longPressFired = true;
      toggleSheet(true);
    }, 650);
  });
  const cancelLongPress = () => {
    if (!state.longPressTimer) {
      return;
    }
    clearTimeout(state.longPressTimer);
    state.longPressTimer = null;
  };
  el.pulse.addEventListener("pointerup", cancelLongPress);
  el.pulse.addEventListener("pointercancel", cancelLongPress);
  el.pulse.addEventListener("pointerleave", cancelLongPress);
  el.pulse.addEventListener("contextmenu", (evt) => {
    evt.preventDefault();
    toggleSheet(true);
  });
  el.pulse.addEventListener("click", () => {
    if (state.longPressFired) {
      state.longPressFired = false;
      return;
    }
    void toggleMic();
  });

  el.closeSheetBtn.addEventListener("click", () => {
    toggleSheet(false);
  });
  el.sheet.addEventListener("click", (evt) => {
    if (evt.target === el.sheet) {
      toggleSheet(false);
    }
  });

  el.userId.addEventListener("change", () => {
    persistPrefs();
  });
  el.persona.addEventListener("change", () => {
    persistPrefs();
  });
  if (el.voice) {
    el.voice.addEventListener("change", () => {
      state.voiceId = (el.voice.value || "").trim();
      persistPrefs();
      setCaption("Voice saved.", "Tap New session to apply.", { clearAfterMs: 2200 });
    });
  }
  if (el.handsFree) {
    el.handsFree.addEventListener("change", () => {
      persistPrefs();
      void applyHandsFreeMode();
    });
  }

  el.newSessionBtn.addEventListener("click", () => {
    void resetSession({ keepConnected: true });
  });
  if (el.previewVoiceBtn) {
    el.previewVoiceBtn.addEventListener("click", () => {
      void previewSelectedVoice();
    });
  }
  if (el.stopMicBtn) {
    el.stopMicBtn.addEventListener("click", () => {
      void stopMic({ sendStop: false });
    });
  }
  el.disconnectBtn.addEventListener("click", () => {
    void disconnect();
  });
  el.endSessionBtn.addEventListener("click", () => {
    void endSession();
  });

  window.addEventListener("keydown", (evt) => {
    if (evt.repeat) {
      return;
    }
    unlockAudio();
    if (evt.key === " " || evt.code === "Space") {
      evt.preventDefault();
      void toggleMic();
      return;
    }
    if (evt.key === "Escape") {
      evt.preventDefault();
      stopPlayback({ clearBuffered: true });
      sendControl("interrupt");
      setCaption("…", "Interrupting…", { clearAfterMs: 900 });
      return;
    }
    if (evt.key === "s" || evt.key === "S") {
      evt.preventDefault();
      toggleSheet(!isSheetOpen());
      return;
    }
    if (evt.key === "d" || evt.key === "D") {
      evt.preventDefault();
      toggleDebug();
    }
  });
}

function wireOnboarding() {
  if (!el.onboard) {
    return;
  }

  el.onboardClose?.addEventListener("click", () => {
    closeOnboarding({ markComplete: true });
  });

  el.onboard.addEventListener("click", (evt) => {
    if (evt.target === el.onboard) {
      closeOnboarding({ markComplete: true });
    }
  });

  el.onboardSettingsBtn?.addEventListener("click", () => {
    closeOnboarding({ markComplete: false });
    toggleSheet(true);
  });

  el.onboardHearBtn?.addEventListener("click", () => {
    void previewSelectedVoice();
  });

  el.onboardMicBtn?.addEventListener("click", () => {
    void (async () => {
      try {
        await requestMicPermission();
        await refreshMicPermission();
        renderOnboarding();
        setCaption("Microphone enabled.", "Press Space to talk.", { clearAfterMs: 2600 });
      } catch (err) {
        renderOnboarding();
        setCaption("Microphone blocked.", stringifyError(err), { clearAfterMs: 4200 });
      }
    })();
  });

  el.onboardHandsFree?.addEventListener("change", () => {
    const enabled = Boolean(el.onboardHandsFree?.checked);
    if (el.handsFree) {
      el.handsFree.checked = enabled;
    }
    persistPrefs();
    void applyHandsFreeMode();
  });
}

async function initOnboarding() {
  await refreshMicPermission();

  try {
    const res = await fetch("/v1/onboarding/status", { cache: "no-store" });
    if (res.ok) {
      state.onboardingStatus = await res.json();
    }
  } catch (_err) {
    // ignore
  }

  renderOnboarding();

  const onboarded = localStorage.getItem("samantha.onboarded") === "1";
  const qs = new URLSearchParams(window.location.search || "");
  const forced = qs.has("onboarding") || (window.location.hash || "") === "#onboarding";
  const hasError = Boolean(
    state.onboardingStatus &&
      Array.isArray(state.onboardingStatus.checks) &&
      state.onboardingStatus.checks.some((c) => String(c.status || "") === "error"),
  );

  if (forced || !onboarded || hasError) {
    openOnboarding(true);
  }
}

function openOnboarding(open) {
  if (!el.onboard) {
    return;
  }
  state.onboardingOpen = Boolean(open);
  el.onboard.classList.toggle("hidden", !state.onboardingOpen);
  if (state.onboardingOpen) {
    renderOnboarding();
  }
}

function closeOnboarding({ markComplete }) {
  if (markComplete) {
    localStorage.setItem("samantha.onboarded", "1");
  }
  openOnboarding(false);
}

function renderOnboarding() {
  if (!el.onboardChecks) {
    return;
  }
  const root = el.onboardChecks;
  root.innerHTML = "";

  const rows = [];

  const micState = String(state.micPermission || "unknown");
  const micRow = {
    id: "mic_permission",
    status: micState === "granted" ? "ok" : micState === "denied" ? "error" : "warn",
    label: "Microphone",
    detail:
      micState === "granted"
        ? "permission granted"
        : micState === "denied"
          ? "blocked by browser settings"
          : micState === "prompt"
            ? "needs permission"
            : "permission unknown",
    fix:
      micState === "denied"
        ? "Allow microphone access in your browser site settings, then reload."
        : micState === "prompt"
          ? "Click “Enable microphone”."
          : "",
  };
  rows.push(micRow);

  const status = state.onboardingStatus;
  if (status && Array.isArray(status.checks)) {
    for (const c of status.checks) {
      rows.push({
        id: String(c.id || ""),
        status: String(c.status || "warn"),
        label: String(c.label || ""),
        detail: String(c.detail || ""),
        fix: String(c.fix || ""),
      });
    }
  } else {
    rows.push({
      id: "server_status",
      status: "warn",
      label: "Server",
      detail: "Unable to fetch onboarding status.",
      fix: "",
    });
  }

  for (const row of rows) {
    if (!row.label) {
      continue;
    }
    const wrap = document.createElement("div");
    const statusClass = row.status === "ok" ? "is-ok" : row.status === "error" ? "is-error" : "is-warn";
    wrap.className = `check-row ${statusClass}`;

    const dot = document.createElement("div");
    dot.className = "check-dot";

    const text = document.createElement("div");
    text.className = "check-text";

    const label = document.createElement("div");
    label.className = "check-label";
    label.textContent = row.label;

    const detail = document.createElement("div");
    detail.className = "check-detail";
    detail.textContent = row.detail || "";

    text.appendChild(label);
    if (row.detail) {
      text.appendChild(detail);
    }

    wrap.appendChild(dot);
    wrap.appendChild(text);

    root.appendChild(wrap);

    if (row.fix) {
      const fix = document.createElement("div");
      fix.className = "check-fix";
      fix.textContent = row.fix;
      root.appendChild(fix);
    }
  }

  if (el.onboardMicBtn) {
    const denied = String(state.micPermission || "") === "denied";
    el.onboardMicBtn.disabled = denied;
    el.onboardMicBtn.textContent = denied ? "Microphone blocked" : "Enable microphone";
  }
}

async function refreshMicPermission() {
  state.micPermission = "unknown";
  if (!navigator.permissions || !navigator.permissions.query) {
    return;
  }
  try {
    // Not all browsers support querying "microphone".
    const perm = await navigator.permissions.query({ name: "microphone" });
    state.micPermission = perm.state || "unknown";
    perm.onchange = () => {
      state.micPermission = perm.state || "unknown";
      renderOnboarding();
    };
  } catch (_err) {
    // ignore
  }
}

async function requestMicPermission() {
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
    throw new Error("getUserMedia not supported");
  }
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
  });
  try {
    for (const track of stream.getTracks()) {
      track.stop();
    }
  } catch (_err) {
    // ignore
  }
}

function hydratePrefs() {
  const savedUserId = localStorage.getItem("samantha.userId") || "antonio";
  const savedPersona = localStorage.getItem("samantha.persona") || "warm";
  const savedVoiceId = localStorage.getItem("samantha.voiceId") || "";
  const savedHandsFree = localStorage.getItem("samantha.handsFree") === "1";
  el.userId.value = savedUserId;
  el.persona.value = savedPersona;
  state.voiceId = savedVoiceId;
  if (el.handsFree) {
    el.handsFree.checked = savedHandsFree;
  }
  if (el.onboardHandsFree) {
    el.onboardHandsFree.checked = savedHandsFree;
  }
  state.handsFree = savedHandsFree;
}

function persistPrefs() {
  localStorage.setItem("samantha.userId", (el.userId.value || "").trim() || "anonymous");
  localStorage.setItem("samantha.persona", (el.persona.value || "").trim() || "warm");
  if ((state.voiceId || "").trim()) {
    localStorage.setItem("samantha.voiceId", (state.voiceId || "").trim());
  }
  if (el.handsFree) {
    localStorage.setItem("samantha.handsFree", el.handsFree.checked ? "1" : "0");
    state.handsFree = el.handsFree.checked;
  }
}

async function applyHandsFreeMode() {
  if (!state.connected) {
    return;
  }
  // Hands-free is a server-side gate: we keep sending audio, but the backend will only
  // respond after the wake word (or after we arm a one-off push-to-talk fallback).
  if (state.handsFree) {
    sendControl("wakeword_on");
    if (!state.micActive) {
      await startMic();
    }
    if (state.connected && state.micActive) {
      setPresence("connected", "Samantha", "");
    }
    return;
  }

  sendControl("wakeword_off");
  state.awakeUntilMs = 0;
  state.manualArmUntilMs = 0;
  resetVAD();
  if (state.micActive) {
    await stopMic({ sendStop: false });
  }
}

function armManualOnce() {
  if (!state.sessionId) {
    return;
  }
  const until = Date.now() + 12_000;
  state.manualArmUntilMs = until;
  state.awakeUntilMs = Math.max(state.awakeUntilMs, until);
  sendControl("manual_arm");
  setPresence("listening", "I’m listening.", "");
}

function isAwake() {
  if (!state.handsFree) {
    return true;
  }
  const now = Date.now();
  return now < state.awakeUntilMs || now < state.manualArmUntilMs;
}

function expireArms() {
  const now = Date.now();
  if (state.manualArmUntilMs && now >= state.manualArmUntilMs) {
    state.manualArmUntilMs = 0;
  }
  if (state.awakeUntilMs && now >= state.awakeUntilMs) {
    state.awakeUntilMs = 0;
  }
}

function startEnergyLoop() {
  let last = performance.now();
  const tick = (now) => {
    const dt = Math.max(0, (now - last) / 1000);
    last = now;

    // Smooth target energy (mic energy + speaking boost).
    const target = clamp01(Math.max(state.micEnergyTarget, state.speakEnergyTarget));
    const k = 1 - Math.exp(-dt * 9.5);
    state.energy = state.energy + (target - state.energy) * k;
    if (state.speakEnergyTarget > 0) {
      state.speakEnergyTarget = Math.max(0, state.speakEnergyTarget - dt * 0.9);
    }
    el.pulse.style.setProperty("--energy", state.energy.toFixed(4));
    // Also drive global ambient UI (background glow, etc).
    document.documentElement.style.setProperty("--energy", state.energy.toFixed(4));
    drawViz(now);

    expireArms();
    maybeAutoCommit();
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

function drawViz(nowMs) {
  if (!state.vizCtx || !el.viz) {
    return;
  }
  resizeViz();
  const ctx = state.vizCtx;
  const w = state.vizCSSW || el.viz.clientWidth || window.innerWidth || 1;
  const h = state.vizCSSH || el.viz.clientHeight || window.innerHeight || 1;

  // Clear.
  ctx.clearRect(0, 0, w, h);

  const presence = state.presence || "connected";
  const mic = clamp01(state.micEnergyTarget * 3.2);
  const speak = clamp01(state.speakEnergyTarget * 3.0);
  const e = clamp01(mic * 0.85 + speak * 0.9 + state.energy * 0.2);

  let alpha = 0.42;
  let amp = lerp(0.9, 18, Math.pow(e, 0.85));
  let speed = 0.55;
  let density = 1.0;
  if (presence === "disconnected") {
    alpha = 0.12;
    amp = 0.65;
    speed = 0.2;
    density = 0.7;
  } else if (presence === "connected") {
    alpha = 0.22;
    amp = lerp(0.75, 5.5, Math.pow(e, 0.8));
    speed = 0.28;
    density = 0.85;
  } else if (presence === "listening") {
    alpha = 0.55;
    const t = clamp01(mic * 0.95 + e * 0.25);
    amp = lerp(1.4, 22, Math.pow(t, 0.78));
    speed = 1.15;
    density = 1.1;
  } else if (presence === "thinking") {
    alpha = 0.4;
    amp = lerp(1.2, 12, clamp01(e * 0.6 + 0.25));
    speed = 0.85;
    density = 1.05;
  } else if (presence === "speaking") {
    alpha = 0.62;
    const t = clamp01(speak * 0.95 + e * 0.35);
    amp = lerp(1.8, 24, Math.pow(t, 0.82));
    speed = 1.55;
    density = 1.15;
  } else if (presence === "error") {
    alpha = 0.35;
    amp = lerp(1.0, 9, clamp01(e * 0.5 + 0.2));
    speed = 0.6;
  }

  const t = state.vizReducedMotion ? 0 : (nowMs - state.vizStartMs) / 1000;
  const TAU = Math.PI * 2;
  const cx = w * 0.5;
  const cy = h * 0.47;
  const lineW = Math.min(w * 0.68, 820);
  const left = cx - lineW / 2;
  const points = Math.floor(clamp((lineW / 3) * density, 160, 320));

  const f1 = lerp(0.9, 1.6, clamp01(e * 0.9 + 0.1));
  const f2 = lerp(2.6, 4.2, clamp01(e * 0.9));
  const f3 = lerp(6.5, 9.0, clamp01(e));
  const seed = state.vizSeed;

  const waveAt = (u) => {
    const env = Math.pow(Math.sin(u * Math.PI), 1.15);
    const a = amp * env;
    const s =
      Math.sin(u * TAU * f1 + t * speed * 1.05 + seed) * 0.62 +
      Math.sin(u * TAU * f2 - t * speed * 0.78 + seed * 0.7) * 0.32 +
      Math.sin(u * TAU * f3 + t * speed * 1.45 + seed * 1.3) * 0.18;
    return s * a;
  };

  const grad = ctx.createLinearGradient(left, cy, left + lineW, cy);
  grad.addColorStop(0, `rgba(103, 232, 249, ${alpha * 0.0})`);
  grad.addColorStop(0.2, `rgba(103, 232, 249, ${alpha * 0.95})`);
  grad.addColorStop(0.5, `rgba(243, 248, 255, ${alpha * 0.9})`);
  grad.addColorStop(0.8, `rgba(255, 106, 75, ${alpha * 0.85})`);
  grad.addColorStop(1, `rgba(255, 106, 75, ${alpha * 0.0})`);

  // Glow pass.
  ctx.save();
  ctx.globalCompositeOperation = "lighter";
  ctx.shadowColor = `rgba(103, 232, 249, ${alpha * 0.55})`;
  ctx.shadowBlur = 18 + e * 40;
  ctx.lineWidth = 2.8;
  ctx.strokeStyle = grad;
  ctx.beginPath();
  for (let i = 0; i < points; i += 1) {
    const u = points <= 1 ? 0 : i / (points - 1);
    const x = left + u * lineW;
    const y = cy + waveAt(u);
    if (i === 0) {
      ctx.moveTo(x, y);
    } else {
      ctx.lineTo(x, y);
    }
  }
  ctx.stroke();
  ctx.restore();

  // Core pass.
  ctx.save();
  ctx.globalCompositeOperation = "source-over";
  ctx.shadowBlur = 0;
  ctx.lineWidth = 1.2;
  ctx.strokeStyle = grad;
  ctx.globalAlpha = 0.92;
  ctx.beginPath();
  for (let i = 0; i < points; i += 1) {
    const u = points <= 1 ? 0 : i / (points - 1);
    const x = left + u * lineW;
    const y = cy + waveAt(u);
    if (i === 0) {
      ctx.moveTo(x, y);
    } else {
      ctx.lineTo(x, y);
    }
  }
  ctx.stroke();
  ctx.restore();

  // Thinking: add a single scanning "spark" to feel alive without adding UI chrome.
  if (presence === "thinking" && !state.vizReducedMotion) {
    const u = (t * 0.22) % 1;
    const x = left + u * lineW;
    const y = cy + waveAt(u);
    ctx.save();
    ctx.globalCompositeOperation = "lighter";
    ctx.shadowColor = `rgba(243, 248, 255, ${alpha})`;
    ctx.shadowBlur = 18;
    ctx.fillStyle = `rgba(243, 248, 255, ${alpha * 0.9})`;
    ctx.beginPath();
    ctx.arc(x, y, 1.8 + e * 1.4, 0, TAU);
    ctx.fill();
    ctx.restore();
  }
}

function wirePointerParallax() {
  if (window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    return;
  }
  const root = document.documentElement;
  let targetX = 0.5;
  let targetY = 0.4;
  let x = targetX;
  let y = targetY;
  let raf = 0;

  const apply = () => {
    raf = 0;
    const k = 0.12;
    x += (targetX - x) * k;
    y += (targetY - y) * k;
    root.style.setProperty("--mx", `${(x * 100).toFixed(2)}%`);
    root.style.setProperty("--my", `${(y * 100).toFixed(2)}%`);
    if (Math.abs(targetX - x) + Math.abs(targetY - y) > 0.002) {
      raf = requestAnimationFrame(apply);
    }
  };

  const setTarget = (evt) => {
    const w = window.innerWidth || 1;
    const h = window.innerHeight || 1;
    const nx = clamp01(evt.clientX / w);
    const ny = clamp01(evt.clientY / h);
    targetX = nx;
    targetY = ny;
    if (!raf) {
      raf = requestAnimationFrame(apply);
    }
  };

  window.addEventListener("pointermove", setTarget, { passive: true });
  window.addEventListener("pointerdown", setTarget, { passive: true });
  window.addEventListener("blur", () => {
    targetX = 0.5;
    targetY = 0.4;
    if (!raf) {
      raf = requestAnimationFrame(apply);
    }
  });

  // Initialize once.
  root.style.setProperty("--mx", "50%");
  root.style.setProperty("--my", "40%");
}

function maybeAutoCommit() {
  if (!state.micActive || !state.connected) {
    return;
  }
  // Only auto-commit when we're actively streaming an utterance.
  if (!state.vadStreaming) {
    return;
  }
  if (state.presence === "speaking" && !state.bargeInActive) {
    return;
  }
  if (!state.sawSpeech || !state.lastVoiceAtMs) {
    return;
  }
  const now = Date.now();
  // Tune for "talk, then pause": commit after a short silence.
  const silenceMs = now - state.lastVoiceAtMs;
  if (silenceMs < 650) {
    return;
  }
  // Avoid spamming commit during long silences.
  if (now - state.lastAutoCommitAtMs < 1200) {
    return;
  }

  state.lastAutoCommitAtMs = now;
  state.sawSpeech = false;
  state.vadStreaming = false;
  state.vadSpeechFrames = 0;
  state.vadSilenceFrames = 0;
  clearVADPreroll();
  sendControl("stop");
}

async function ensureSessionConnected() {
  if (!state.sessionId) {
    await createSession();
  }
  if (!state.connected) {
    await connect();
  }
}

async function resetSession({ keepConnected }) {
  await stopMic({ sendStop: false });
  stopPlayback({ clearBuffered: true });
  state.bargeInActive = false;
  state.bargeInFrames = 0;
  clearBargePreroll();
  if (state.ignoredTurns) {
    state.ignoredTurns.clear();
  }
  const prior = state.sessionId;
  state.sessionId = "";
  if (prior) {
    try {
      await fetch(`/v1/voice/session/${encodeURIComponent(prior)}/end`, { method: "POST" });
    } catch (_err) {
      // ignore
    }
  }

  if (keepConnected) {
    await disconnect();
    await createSession();
    await connect();
  }
  toggleSheet(false);
}

async function createSession() {
  const userID = (el.userId.value || "").trim() || "anonymous";
  const personaID = (el.persona.value || "").trim() || "warm";
  persistPrefs();
  const voiceID = (state.voiceId || "").trim();

  const res = await fetch("/v1/voice/session", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ user_id: userID, persona_id: personaID, voice_id: voiceID }),
  });
  if (!res.ok) {
    setPresence("error", "Samantha", `Create session failed (${res.status})`);
    throw new Error(`create session failed: HTTP ${res.status}`);
  }
  const payload = await res.json();
  state.sessionId = payload.session_id || "";
  if (typeof payload.voice_id === "string" && payload.voice_id.trim()) {
    state.voiceId = payload.voice_id.trim();
    // Keep prefs in sync even if the backend applied a default.
    localStorage.setItem("samantha.voiceId", state.voiceId);
    if (el.voice && el.voice.value !== state.voiceId) {
      el.voice.value = state.voiceId;
    }
  }
  state.seq = 0;
  logEvent(`session created: ${state.sessionId}`);
  return state.sessionId;
}

async function connect() {
  if (!state.sessionId || state.connected) {
    return;
  }
  if (state.reconnectTimer) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }

  const scheme = window.location.protocol === "https:" ? "wss" : "ws";
  const wsURL = `${scheme}://${window.location.host}/v1/voice/session/ws?session_id=${encodeURIComponent(
    state.sessionId,
  )}`;

  setPresence("connected", "Samantha", "Ready.");

  const ws = new WebSocket(wsURL);
  state.ws = ws;

  ws.addEventListener("open", () => {
    state.connected = true;
    state.reconnectBackoffMs = 250;
    logEvent("ws connected");
    setPresence("connected", "Samantha", "");
    void applyHandsFreeMode();
  });

  ws.addEventListener("message", (evt) => {
    handleServerMessage(evt.data);
  });

  ws.addEventListener("close", () => {
    state.connected = false;
    state.ws = null;
    void stopMic({ sendStop: false });
    stopPlayback({ clearBuffered: true });
    state.bargeInActive = false;
    state.bargeInFrames = 0;
    clearBargePreroll();
    if (!document.hidden) {
      scheduleReconnect();
    }
    logEvent("ws disconnected");
  });

  ws.addEventListener("error", () => {
    // Close event will follow; keep it quiet.
  });
}

function scheduleReconnect() {
  if (state.reconnectTimer) {
    return;
  }
  const delay = clamp(state.reconnectBackoffMs, 250, 4000);
  state.reconnectBackoffMs = Math.min(4000, Math.floor(state.reconnectBackoffMs * 1.7));
  state.reconnectTimer = setTimeout(() => {
    state.reconnectTimer = null;
    void reconnectFlow();
  }, delay);
}

async function reconnectFlow() {
  try {
    await disconnect();
  } catch (_err) {
    // ignore
  }
  try {
    await createSession();
    await connect();
  } catch (err) {
    setPresence("error", "Samantha", stringifyError(err));
    scheduleReconnect();
  }
}

async function disconnect() {
  await stopMic({ sendStop: false });
  stopPlayback({ clearBuffered: true });
  state.bargeInActive = false;
  state.bargeInFrames = 0;
  clearBargePreroll();
  if (state.ignoredTurns) {
    state.ignoredTurns.clear();
  }
  if (state.ws) {
    state.ws.close();
  }
  state.connected = false;
  state.ws = null;
  setPresence("disconnected", "Samantha", "Disconnected.");
}

async function endSession() {
  const sessionId = state.sessionId;
  if (!sessionId) {
    return;
  }
  await disconnect();
  try {
    await fetch(`/v1/voice/session/${encodeURIComponent(sessionId)}/end`, { method: "POST" });
  } catch (_err) {
    // ignore
  }
  state.sessionId = "";
  logEvent("session ended");
  setPresence("disconnected", "Samantha", "Session ended.");
  toggleSheet(false);
}

async function toggleMic() {
  if (!state.connected) {
    await ensureSessionConnected();
  }
  if (!state.connected) {
    return;
  }
  if (state.handsFree) {
    if (!state.micActive) {
      await startMic();
      return;
    }
    armManualOnce();
    return;
  }
  if (state.micActive) {
    await stopMic({ sendStop: true });
    return;
  }
  await startMic();
}

function isSheetOpen() {
  return !el.sheet.classList.contains("hidden");
}

function toggleSheet(open) {
  if (open) {
    el.sheet.classList.remove("hidden");
    void loadVoicesOnce();
    el.userId.focus();
    return;
  }
  el.sheet.classList.add("hidden");
}

async function loadVoicesOnce() {
  if (!el.voice) {
    return;
  }
  if (state.voicesLoaded || state.voicesLoading) {
    return;
  }
  state.voicesLoading = true;
  try {
    const res = await fetch("/v1/voice/voices", { cache: "no-store" });
    if (!res.ok) {
      throw new Error(`voices request failed: HTTP ${res.status}`);
    }
    const data = await res.json();

    const all = Array.isArray(data.voices) ? data.voices : [];
    const recommended = Array.isArray(data.recommended) ? data.recommended : [];
    const defaultVoiceID = typeof data.default_voice_id === "string" ? data.default_voice_id.trim() : "";

    el.voice.innerHTML = "";

    const addOption = (voice, { prefix }) => {
      const id = String(voice.voice_id || "").trim();
      const name = String(voice.name || "").trim();
      if (!id || !name) {
        return;
      }
      const opt = document.createElement("option");
      opt.value = id;
      opt.textContent = prefix ? `${prefix}${name}` : name;
      el.voice.appendChild(opt);
    };

    if (recommended.length > 0) {
      const group = document.createElement("optgroup");
      group.label = "Recommended";
      for (const v of recommended) {
        const opt = document.createElement("option");
        opt.value = String(v.voice_id || "").trim();
        opt.textContent = String(v.name || "").trim();
        if (opt.value && opt.textContent) {
          group.appendChild(opt);
        }
      }
      if (group.children.length > 0) {
        el.voice.appendChild(group);
      }
    }

    if (all.length > 0) {
      const group = document.createElement("optgroup");
      group.label = "All (female)";
      for (const v of all) {
        const opt = document.createElement("option");
        opt.value = String(v.voice_id || "").trim();
        opt.textContent = String(v.name || "").trim();
        if (opt.value && opt.textContent) {
          group.appendChild(opt);
        }
      }
      if (group.children.length > 0) {
        el.voice.appendChild(group);
      }
    }

    if (el.voice.options.length === 0) {
      const opt = document.createElement("option");
      opt.value = "";
      opt.textContent = "Set ELEVENLABS_API_KEY to load voices";
      el.voice.appendChild(opt);
    }

    const saved = (state.voiceId || "").trim() || (localStorage.getItem("samantha.voiceId") || "").trim();
    let next = "";
    if (saved && hasVoiceOption(saved)) {
      next = saved;
    } else if (defaultVoiceID && hasVoiceOption(defaultVoiceID)) {
      next = defaultVoiceID;
    } else if (recommended.length > 0 && hasVoiceOption(String(recommended[0].voice_id || "").trim())) {
      next = String(recommended[0].voice_id || "").trim();
    } else {
      next = (el.voice.options[0]?.value || "").trim();
    }

    if (next) {
      el.voice.value = next;
      state.voiceId = next;
      localStorage.setItem("samantha.voiceId", next);
    }

    state.voicesLoaded = true;
  } catch (err) {
    logError(`voices load error: ${stringifyError(err)}`);
    el.voice.innerHTML = "";
    const opt = document.createElement("option");
    opt.value = "";
    opt.textContent = "Unable to load voices";
    el.voice.appendChild(opt);
  } finally {
    state.voicesLoading = false;
  }
}

function hasVoiceOption(voiceID) {
  if (!el.voice) {
    return false;
  }
  const id = String(voiceID || "").trim();
  if (!id) {
    return false;
  }
  for (const opt of el.voice.options) {
    if (String(opt.value || "").trim() === id) {
      return true;
    }
  }
  return false;
}

async function previewSelectedVoice() {
  await loadVoicesOnce();

  const voiceID = (state.voiceId || (el.voice ? el.voice.value : "") || "").trim();
  if (!voiceID) {
    setCaption("No voice selected.", "", { clearAfterMs: 1800 });
    return;
  }

  const personaID = (el.persona.value || "").trim() || "warm";
  const userID = (el.userId.value || "").trim() || "there";
  const voiceName = (el.voice && el.voice.selectedOptions && el.voice.selectedOptions[0])
    ? (el.voice.selectedOptions[0].textContent || "").trim()
    : "voice";

  // Keep this short to make auditioning fast + cheap.
  const text = `Hi ${userID}. I’m here. Tell me what you’re thinking.`;

  const restorePresence = () => {
    if (state.micActive && state.handsFree) {
      setPresence("connected", "Samantha", "");
      return;
    }
    if (state.micActive) {
      setPresence("listening", "I’m listening.", "");
      return;
    }
    if (state.connected) {
      setPresence("connected", "Samantha", "Ready.");
      return;
    }
    setPresence("disconnected", "Samantha", "Disconnected.");
  };

  setPresence("speaking", `Previewing ${voiceName}…`, "");
  setCaption(`Previewing ${voiceName}…`, "", { clearAfterMs: 0 });
  state.speakEnergyTarget = Math.max(state.speakEnergyTarget, 0.46);

  let restoreAfter = true;
  try {
    const res = await fetch("/v1/voice/tts/preview", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ voice_id: voiceID, persona_id: personaID, text }),
    });
    if (!res.ok) {
      const detail = await safeReadText(res);
      throw new Error(`preview failed: HTTP ${res.status}${detail ? `: ${detail}` : ""}`);
    }
    const contentType = res.headers.get("content-type") || "";
    const buf = await res.arrayBuffer();
    enqueueAudioPlayback(new Uint8Array(buf), contentType);
    await state.audioQueue;
  } catch (err) {
    setPresence("error", "Preview failed.", stringifyError(err));
    logError(`voice preview error: ${stringifyError(err)}`);
    restoreAfter = false;
  } finally {
    if (restoreAfter) {
      restorePresence();
    }
  }
}

async function safeReadText(res) {
  try {
    return await res.text();
  } catch (_err) {
    return "";
  }
}

function toggleDebug() {
  if (el.debug.classList.contains("hidden")) {
    el.debug.classList.remove("hidden");
    logEvent("debug opened");
    return;
  }
  el.debug.classList.add("hidden");
  logEvent("debug closed");
}

function sendJSON(payload) {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    return false;
  }
  state.ws.send(JSON.stringify(payload));
  return true;
}

function sendControl(action) {
  if (!state.sessionId) {
    return;
  }
  const ok = sendJSON({
    type: "client_control",
    session_id: state.sessionId,
    action,
  });
  if (ok) {
    logEvent(`control sent: ${action}`);
  }
}

function clearBargePreroll() {
  state.bargePreroll = [];
  state.bargePrerollBytes = 0;
}

function pushBargePreroll(pcmBytes) {
  if (!pcmBytes || pcmBytes.length === 0) {
    return;
  }
  state.bargePreroll.push(pcmBytes);
  state.bargePrerollBytes += pcmBytes.length;
  while (state.bargePrerollBytes > BARGE_PREROLL_MAX_BYTES && state.bargePreroll.length > 1) {
    const drop = state.bargePreroll.shift();
    if (drop) {
      state.bargePrerollBytes -= drop.length;
    }
  }
}

function flushBargePreroll() {
  if (!state.bargePreroll || state.bargePreroll.length === 0) {
    return;
  }
  const chunks = state.bargePreroll;
  clearBargePreroll();
  for (const chunk of chunks) {
    if (!chunk || chunk.length === 0) {
      continue;
    }
    sendJSON({
      type: "client_audio_chunk",
      session_id: state.sessionId,
      seq: ++state.seq,
      pcm16_base64: bytesToBase64(chunk),
      sample_rate: MIC_TARGET_SAMPLE_RATE,
      ts_ms: Date.now(),
    });
  }
}

function clearVADPreroll() {
  state.vadPreroll = [];
  state.vadPrerollBytes = 0;
}

function pushVADPreroll(pcmBytes) {
  if (!pcmBytes || pcmBytes.length === 0) {
    return;
  }
  state.vadPreroll.push(pcmBytes);
  state.vadPrerollBytes += pcmBytes.length;
  while (state.vadPrerollBytes > VAD_PREROLL_MAX_BYTES && state.vadPreroll.length > 1) {
    const drop = state.vadPreroll.shift();
    if (drop) {
      state.vadPrerollBytes -= drop.length;
    }
  }
}

function flushVADPreroll() {
  if (!state.vadPreroll || state.vadPreroll.length === 0) {
    return;
  }
  const chunks = state.vadPreroll;
  clearVADPreroll();
  for (const chunk of chunks) {
    if (!chunk || chunk.length === 0) {
      continue;
    }
    sendJSON({
      type: "client_audio_chunk",
      session_id: state.sessionId,
      seq: ++state.seq,
      pcm16_base64: bytesToBase64(chunk),
      sample_rate: MIC_TARGET_SAMPLE_RATE,
      ts_ms: Date.now(),
    });
  }
}

function resetVAD() {
  state.lastVoiceAtMs = 0;
  state.sawSpeech = false;
  state.lastAutoCommitAtMs = 0;
  state.vadNoiseRMS = 0;
  state.vadSpeechFrames = 0;
  state.vadSilenceFrames = 0;
  state.vadStreaming = false;
  clearVADPreroll();
}

function analyzeAudioFrame(float32) {
  if (!float32 || float32.length === 0) {
    return { rms: 0, peak: 0, zcr: 0 };
  }
  let sum = 0;
  let peak = 0;
  let crossings = 0;
  let prevSign = float32[0] >= 0;
  for (let i = 0; i < float32.length; i += 1) {
    const v = float32[i];
    sum += v * v;
    const av = Math.abs(v);
    if (av > peak) {
      peak = av;
    }
    if (i === 0) {
      continue;
    }
    const sign = v >= 0;
    if (sign !== prevSign) {
      crossings += 1;
      prevSign = sign;
    }
  }
  const rms = Math.sqrt(sum / float32.length);
  const zcr = crossings / Math.max(1, float32.length - 1);
  return { rms, peak, zcr };
}

function vadThreshold() {
  const noise = state.vadNoiseRMS || 0;
  const dyn = noise > 0 ? noise * VAD_RELATIVE_THRESHOLD : 0;
  return clamp(Math.max(VAD_MIN_RMS, dyn), VAD_MIN_RMS, 0.2);
}

function isSpeechLike(metrics) {
  const rms = metrics?.rms || 0;
  if (!rms) {
    return false;
  }
  const th = vadThreshold();
  if (rms <= th) {
    return false;
  }

  // Reject clicky/impulse noise (very high peak-to-average).
  const peak = metrics?.peak || 0;
  const crest = peak / (rms + 1e-6);
  if (crest > VAD_MAX_CREST && rms < th * 2.8) {
    return false;
  }

  // Reject very noise-like frames (random sign flips / excessive high-frequency energy).
  const zcr = metrics?.zcr || 0;
  if (zcr > VAD_MAX_ZCR && rms < th * 2.2) {
    return false;
  }

  return true;
}

function updateNoiseFloor(rms, speechLike) {
  if (!rms || speechLike) {
    return;
  }
  const prev = state.vadNoiseRMS || 0;
  if (!prev) {
    state.vadNoiseRMS = clamp(rms, 0.0005, 0.05);
    return;
  }
  const ratio = rms / (prev + 1e-6);
  const alpha = ratio < 1.4 ? 0.08 : 0.008;
  state.vadNoiseRMS = clamp(prev + (rms - prev) * alpha, 0.0005, 0.05);
}


function decodeAudio(ctx, arrayBuffer) {
  return new Promise((resolve, reject) => {
    try {
      const maybePromise = ctx.decodeAudioData(arrayBuffer, resolve, reject);
      if (maybePromise && typeof maybePromise.then === "function") {
        maybePromise.then(resolve).catch(reject);
      }
    } catch (err) {
      reject(err);
    }
  });
}

async function ensurePlaybackContext() {
  const AudioContextClass = window.AudioContext || window.webkitAudioContext;
  if (!AudioContextClass) {
    throw new Error("Web Audio API not supported");
  }

  if (state.playbackContext && state.playbackGain && state.playbackContext.state !== "closed") {
    if (state.playbackContext.state === "suspended") {
      await state.playbackContext.resume();
    }
    return state.playbackContext;
  }

  let ctx;
  try {
    ctx = new AudioContextClass({ latencyHint: "interactive" });
  } catch (_err) {
    ctx = new AudioContextClass();
  }
  const gain = ctx.createGain();
  gain.gain.value = 1;
  gain.connect(ctx.destination);

  state.playbackContext = ctx;
  state.playbackGain = gain;
  state.playbackNextTime = 0;
  state.playbackSources = new Set();

  if (ctx.state === "suspended") {
    await ctx.resume();
  }
  return ctx;
}

function unlockAudio() {
  // Must be called from a user gesture to satisfy autoplay policies.
  if (state.playbackContext && state.playbackContext.state === "running") {
    return;
  }
  void ensurePlaybackContext().catch((_err) => {
    // Ignore; playback will guide the user on demand.
  });
}

function stopWebAudio() {
  if (state.playbackSources && state.playbackSources.size) {
    for (const entry of state.playbackSources) {
      try {
        entry.src.onended = null;
      } catch (_err) {
        // ignore
      }
      try {
        entry.src.stop(0);
      } catch (_err) {
        // ignore
      }
      try {
        entry.gain.disconnect();
      } catch (_err) {
        // ignore
      }
    }
    state.playbackSources.clear();
  }
  state.playbackNextTime = 0;
}

async function scheduleWavSegment(bytes, token) {
  if (token !== state.playbackToken) {
    return;
  }

  let ctx;
  try {
    ctx = await ensurePlaybackContext();
  } catch (err) {
    setCaption("Tap once to enable audio.", "Then try again.", { clearAfterMs: 3200 });
    logError(`audio context blocked: ${stringifyError(err)}`);
    return;
  }
  if (token !== state.playbackToken) {
    return;
  }

  const masterGain = state.playbackGain;
  if (!masterGain) {
    return;
  }

  const ab = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
  let buffer;
  try {
    buffer = await decodeAudio(ctx, ab);
  } catch (err) {
    logError(`wav decode failed: ${stringifyError(err)}`);
    // Fallback: element playback (may introduce gaps).
    enqueueAudioPlayback(bytes, "audio/wav");
    return;
  }
  if (token !== state.playbackToken) {
    return;
  }

  const t0 = ctx.currentTime;
  const baseStart = t0 + 0.03;
  if (!state.playbackNextTime || state.playbackNextTime < baseStart - 0.1) {
    state.playbackNextTime = baseStart;
  }
  const startAt = Math.max(baseStart, state.playbackNextTime);
  const endAt = startAt + buffer.duration;

  const src = ctx.createBufferSource();
  src.buffer = buffer;

  const segGain = ctx.createGain();
  const fade = Math.min(0.012, buffer.duration * 0.25);
  const fadeInEnd = startAt + fade;
  const fadeOutStart = Math.max(fadeInEnd, endAt - fade);
  segGain.gain.setValueAtTime(0, startAt);
  segGain.gain.linearRampToValueAtTime(1, fadeInEnd);
  segGain.gain.setValueAtTime(1, fadeOutStart);
  segGain.gain.linearRampToValueAtTime(0, endAt);

  src.connect(segGain);
  segGain.connect(masterGain);

  const entry = { src, gain: segGain, token };
  state.playbackSources.add(entry);
  src.onended = () => {
    state.playbackSources.delete(entry);
    try {
      segGain.disconnect();
    } catch (_err) {
      // ignore
    }
  };

  try {
    src.start(startAt);
  } catch (err) {
    state.playbackSources.delete(entry);
    try {
      segGain.disconnect();
    } catch (_err) {
      // ignore
    }
    logError(`wav schedule failed: ${stringifyError(err)}`);
    return;
  }

  state.playbackNextTime = endAt;
}

function enqueueWavStream(bytes) {
  const token = state.playbackToken;
  state.streamScheduleQueue = state.streamScheduleQueue
    .then(() => scheduleWavSegment(bytes, token))
    .catch((err) => {
      // Keep queue alive even if a single segment fails.
      logError(`wav stream error: ${stringifyError(err)}`);
    });
}

function pcmSampleRateFromFormat(format) {
  const f = String(format || "").toLowerCase();
  const idx = f.indexOf("pcm_");
  if (idx < 0) {
    return null;
  }
  const rest = f.slice(idx + 4);
  let digits = "";
  for (let i = 0; i < rest.length; i += 1) {
    const c = rest[i];
    if (c < "0" || c > "9") {
      break;
    }
    digits += c;
  }
  if (!digits) {
    return MIC_TARGET_SAMPLE_RATE;
  }
  const sr = Number.parseInt(digits, 10);
  if (!Number.isFinite(sr) || sr <= 0) {
    return MIC_TARGET_SAMPLE_RATE;
  }
  return sr;
}

async function schedulePCM16Segment(bytes, sampleRate, token) {
  if (token !== state.playbackToken) {
    return;
  }

  let ctx;
  try {
    ctx = await ensurePlaybackContext();
  } catch (err) {
    setCaption("Tap once to enable audio.", "Then try again.", { clearAfterMs: 3200 });
    logError(`audio context blocked: ${stringifyError(err)}`);
    return;
  }
  if (token !== state.playbackToken) {
    return;
  }

  const masterGain = state.playbackGain;
  if (!masterGain) {
    return;
  }

  const frames = Math.floor(bytes.length / 2);
  if (frames <= 0) {
    return;
  }

  const sr = sampleRate && sampleRate > 0 ? sampleRate : MIC_TARGET_SAMPLE_RATE;
  const buffer = ctx.createBuffer(1, frames, sr);
  const ch0 = buffer.getChannelData(0);
  const dv = new DataView(bytes.buffer, bytes.byteOffset, frames * 2);
  for (let i = 0; i < frames; i += 1) {
    ch0[i] = dv.getInt16(i * 2, true) / 32768;
  }

  const t0 = ctx.currentTime;
  const baseStart = t0 + 0.03;
  if (!state.playbackNextTime || state.playbackNextTime < baseStart - 0.1) {
    state.playbackNextTime = baseStart;
  }
  const startAt = Math.max(baseStart, state.playbackNextTime);
  const endAt = startAt + buffer.duration;

  const src = ctx.createBufferSource();
  src.buffer = buffer;

  const segGain = ctx.createGain();
  const fade = Math.min(0.012, buffer.duration * 0.25);
  const fadeInEnd = startAt + fade;
  const fadeOutStart = Math.max(fadeInEnd, endAt - fade);
  segGain.gain.setValueAtTime(0, startAt);
  segGain.gain.linearRampToValueAtTime(1, fadeInEnd);
  segGain.gain.setValueAtTime(1, fadeOutStart);
  segGain.gain.linearRampToValueAtTime(0, endAt);

  src.connect(segGain);
  segGain.connect(masterGain);

  const entry = { src, gain: segGain, token };
  state.playbackSources.add(entry);
  src.onended = () => {
    state.playbackSources.delete(entry);
    try {
      segGain.disconnect();
    } catch (_err) {
      // ignore
    }
  };

  try {
    src.start(startAt);
  } catch (err) {
    state.playbackSources.delete(entry);
    try {
      segGain.disconnect();
    } catch (_err) {
      // ignore
    }
    logError(`pcm schedule failed: ${stringifyError(err)}`);
    return;
  }

  state.playbackNextTime = endAt;
}

function enqueuePCMStream(bytes, sampleRate) {
  const token = state.playbackToken;
  state.streamScheduleQueue = state.streamScheduleQueue
    .then(() => schedulePCM16Segment(bytes, sampleRate, token))
    .catch((err) => {
      logError(`pcm stream error: ${stringifyError(err)}`);
    });
}

function stopPlayback({ clearBuffered } = {}) {
  state.playbackToken += 1;
  state.speakEnergyTarget = 0;
  if (clearBuffered) {
    state.audioByTurn.clear();
    state.audioFormatByTurn.clear();
  }

  if (state.currentAudio) {
    try {
      state.currentAudio.pause();
      state.currentAudio.currentTime = 0;
    } catch (_err) {
      // ignore
    }
    state.currentAudio = null;
  }

  stopWebAudio();
  state.streamScheduleQueue = Promise.resolve();

  // Ensure future playback doesn't wait behind an interrupted element.
  state.audioQueue = Promise.resolve();
}

async function startMic() {
  if (state.micActive) {
    return;
  }
  if (state.handsFree) {
    setPresence("connected", "Samantha", "");
  } else {
    setPresence("listening", "I’m listening.", "Say something.");
  }
  state.micEnergyTarget = 0;
  resetVAD();

  const AudioContextClass = window.AudioContext || window.webkitAudioContext;
  if (!AudioContextClass) {
    setPresence("error", "Samantha", "Web Audio API not supported.");
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    });
    let ctx;
    try {
      ctx = new AudioContextClass({ latencyHint: "interactive" });
    } catch (_err) {
      ctx = new AudioContextClass();
    }
    const source = ctx.createMediaStreamSource(stream);
    const processor = ctx.createScriptProcessor(MIC_PROCESSOR_BUFFER, 1, 1);
    const silentGain = ctx.createGain();
    silentGain.gain.value = 0;

    processor.onaudioprocess = (e) => {
      if (!state.micActive) {
        return;
      }
      const input = e.inputBuffer.getChannelData(0);
      const metrics = analyzeAudioFrame(input);
      const rms = metrics.rms || 0;
      state.micEnergyTarget = rms * 3.2;

      const downsampled = downsampleFloat32(input, ctx.sampleRate, MIC_TARGET_SAMPLE_RATE);
      const pcmBytes = float32ToPCM16Bytes(downsampled);
      if (pcmBytes.length === 0) {
        return;
      }

      const speechLike = isSpeechLike(metrics);
      if (!state.vadStreaming && state.presence !== "speaking") {
        updateNoiseFloor(rms, speechLike);
      }

      const now = Date.now();

      if (state.presence === "speaking" && !state.bargeInActive) {
        // Default: half-duplex to avoid self-feedback, but keep a short preroll so barge-in
        // doesn't chop off the start of the user's utterance.
        pushBargePreroll(pcmBytes);
        if (speechLike && rms > BARGE_RMS_THRESHOLD) {
          state.bargeInFrames += 1;
          if (state.bargeInFrames >= BARGE_FRAMES_THRESHOLD && now - state.lastBargeSentAtMs > BARGE_COOLDOWN_MS) {
            state.lastBargeSentAtMs = now;
            state.bargeInFrames = 0;
            state.bargeInActive = true;

            // Treat this as a fresh utterance.
            state.vadStreaming = true;
            state.vadSpeechFrames = VAD_ATTACK_FRAMES;
            state.vadSilenceFrames = 0;
            state.sawSpeech = true;
            state.lastVoiceAtMs = now;
            clearVADPreroll();

            stopPlayback({ clearBuffered: true });
            sendControl("interrupt");
            setCaption("…", "Listening…", { clearAfterMs: 1200 });
            // Send the preroll immediately, then continue streaming live audio on the next frames.
            flushBargePreroll();
          }
        } else {
          state.bargeInFrames = Math.max(0, state.bargeInFrames - 1);
        }
        return;
      }

      if (state.presence !== "speaking") {
        state.bargeInFrames = 0;
        clearBargePreroll();
      }

      // Push-to-talk mode: stream continuously (commit is driven by the Stop control).
      if (!state.handsFree) {
        sendJSON({
          type: "client_audio_chunk",
          session_id: state.sessionId,
          seq: ++state.seq,
          pcm16_base64: bytesToBase64(pcmBytes),
          sample_rate: MIC_TARGET_SAMPLE_RATE,
          ts_ms: now,
        });
        return;
      }

      // VAD-gated streaming: only send audio to the backend while we're inside an utterance.
      if (!state.vadStreaming) {
        pushVADPreroll(pcmBytes);
        if (speechLike) {
          state.vadSpeechFrames += 1;
        } else {
          state.vadSpeechFrames = Math.max(0, state.vadSpeechFrames - 1);
        }
        if (state.vadSpeechFrames >= VAD_ATTACK_FRAMES) {
          state.vadStreaming = true;
          state.vadSilenceFrames = 0;
          state.sawSpeech = true;
          state.lastVoiceAtMs = now;
          flushVADPreroll();
        }
        return;
      }

      // Streaming.
      if (speechLike) {
        state.lastVoiceAtMs = now;
        state.vadSilenceFrames = 0;
      } else {
        state.vadSilenceFrames += 1;
      }

      sendJSON({
        type: "client_audio_chunk",
        session_id: state.sessionId,
        seq: ++state.seq,
        pcm16_base64: bytesToBase64(pcmBytes),
        sample_rate: MIC_TARGET_SAMPLE_RATE,
        ts_ms: now,
      });

      if (!speechLike && state.vadSilenceFrames >= VAD_RELEASE_FRAMES) {
        state.lastAutoCommitAtMs = now;
        state.sawSpeech = false;
        state.vadStreaming = false;
        state.vadSpeechFrames = 0;
        state.vadSilenceFrames = 0;
        clearVADPreroll();
        sendControl("stop");
      }
    };

    source.connect(processor);
    processor.connect(silentGain);
    silentGain.connect(ctx.destination);

    state.audioContext = ctx;
    state.mediaStream = stream;
    state.mediaSource = source;
    state.processor = processor;
    state.silentGain = silentGain;
    state.micActive = true;
    logEvent("microphone started");
  } catch (err) {
    setPresence("error", "Samantha", `Mic error: ${stringifyError(err)}`);
    logError(`mic start error: ${stringifyError(err)}`);
  }
}

async function stopMic({ sendStop }) {
  if (!state.micActive && !state.audioContext && !state.mediaStream) {
    return;
  }
  state.micActive = false;
  state.micEnergyTarget = 0;
  state.bargeInActive = false;
  state.bargeInFrames = 0;
  clearBargePreroll();
  state.awakeUntilMs = 0;
  state.manualArmUntilMs = 0;
  resetVAD();

  try {
    if (state.processor) {
      state.processor.disconnect();
    }
    if (state.silentGain) {
      state.silentGain.disconnect();
    }
    if (state.mediaSource) {
      state.mediaSource.disconnect();
    }
    if (state.mediaStream) {
      for (const track of state.mediaStream.getTracks()) {
        track.stop();
      }
    }
    if (state.audioContext) {
      await state.audioContext.close();
    }
  } catch (err) {
    logError(`mic stop error: ${stringifyError(err)}`);
  } finally {
    state.audioContext = null;
    state.mediaStream = null;
    state.mediaSource = null;
    state.processor = null;
    state.silentGain = null;
  }

  if (state.connected) {
    setPresence("connected", "Samantha", state.handsFree ? "Mic off." : "Ready.");
  } else {
    setPresence("disconnected", "Samantha", "Disconnected.");
  }

  if (sendStop && state.sessionId) {
    sendControl("stop");
  }
  logEvent("microphone stopped");
}

function handleServerMessage(raw) {
  let msg;
  try {
    msg = JSON.parse(raw);
  } catch (_err) {
    logError("invalid server message");
    return;
  }

  const t = msg.type || "unknown";
  switch (t) {
    case "stt_partial":
      if (!isAwake()) {
        break;
      }
      setPresence("listening", "I’m listening.", msg.text || "");
      break;
    case "stt_committed":
      if (!isAwake()) {
        break;
      }
      state.bargeInActive = false;
      state.bargeInFrames = 0;
      clearBargePreroll();
      state.awakeUntilMs = 0;
      state.manualArmUntilMs = 0;
      setPresence("thinking", "Thinking…", "");
      setCaption("", msg.text || "", { clearAfterMs: 1200 });
      break;
    case "system_event":
      handleSystemEvent(msg);
      break;
    case "assistant_text_delta":
      if (state.bargeInActive) {
        break;
      }
      if (state.ignoredTurns && state.ignoredTurns.has(msg.turn_id || "")) {
        break;
      }
      setPresence("thinking", "…", "");
      handleAssistantDelta(msg);
      break;
    case "assistant_audio_chunk":
      if (state.bargeInActive) {
        // We're actively barge-ing in: drop late audio so it doesn't fight the mic.
        break;
      }
      if (state.ignoredTurns && state.ignoredTurns.has(msg.turn_id || "")) {
        break;
      }
      state.speakEnergyTarget = Math.max(state.speakEnergyTarget, 0.32);
      setPresence("speaking", "…", "");
      handleAssistantAudio(msg);
      break;
    case "assistant_turn_end":
      handleAssistantTurnEnd(msg);
      break;
    case "error_event":
      setPresence("error", "Something went wrong.", `${msg.code || "error"} (${msg.source || "gateway"})`);
      logError(`error_event ${msg.code || ""} ${msg.detail || ""}`.trim());
      break;
    default:
      logEvent(`event: ${t}`);
      break;
  }
}

function handleSystemEvent(msg) {
  const code = String(msg.code || "").toLowerCase();
  switch (code) {
    case "wake_word":
      state.awakeUntilMs = Date.now() + 8000;
      setPresence("listening", "Yes?", "");
      setCaption("Yes?", "", { clearAfterMs: 1200 });
      logEvent("wake word detected");
      break;
    default:
      logEvent(`system event: ${code || "unknown"}`);
      break;
  }
}

function handleAssistantDelta(msg) {
  const turnID = msg.turn_id || "turn-unknown";
  const delta = msg.text_delta || "";

  if (state.lastTurnID && state.lastTurnID !== turnID) {
    // New turn: reset text.
    state.assistantTextByTurn.delete(state.lastTurnID);
  }
  state.lastTurnID = turnID;

  const prior = state.assistantTextByTurn.get(turnID) || "";
  const next = prior + delta;
  state.assistantTextByTurn.set(turnID, next);

  setCaption("", softTruncate(next, 240), { clearAfterMs: 9000 });
}

function handleAssistantAudio(msg) {
  const turnID = msg.turn_id || "turn-unknown";
  const format = (msg.format || "").toLowerCase();
  if (state.ignoredTurns && state.ignoredTurns.has(turnID)) {
    return;
  }
  if (state.bargeInActive) {
    return;
  }
  if (format.includes("mock_text_bytes")) {
    return;
  }
  const bytes = base64ToBytes(msg.audio_base64 || "");
  if (!bytes.length) {
    return;
  }
  if (format.includes("wav")) {
    // Local TTS emits self-contained WAV segments; schedule them with WebAudio so playback is gapless.
    enqueueWavStream(bytes);
    return;
  }
  const pcmSR = pcmSampleRateFromFormat(format);
  if (pcmSR) {
    // ElevenLabs PCM streams are raw PCM16LE; schedule directly via WebAudio (no buffering until turn end).
    enqueuePCMStream(bytes, pcmSR);
    return;
  }
  const arr = state.audioByTurn.get(turnID) || [];
  arr.push(bytes);
  state.audioByTurn.set(turnID, arr);
  if (format) {
    state.audioFormatByTurn.set(turnID, format);
  }
}

function handleAssistantTurnEnd(msg) {
  const turnID = msg.turn_id || "turn-unknown";
  const reason = msg.reason || "completed";
  const reasonNorm = String(reason || "").trim().toLowerCase();
  const aborted = reasonNorm === "interrupted" || reasonNorm === "barge_in" || reasonNorm === "connection_closed";
  if (aborted) {
    if (state.ignoredTurns) {
      state.ignoredTurns.add(turnID);
      if (state.ignoredTurns.size > 48) {
        state.ignoredTurns.clear();
      }
    }
    state.audioByTurn.delete(turnID);
    state.audioFormatByTurn.delete(turnID);
    stopPlayback({ clearBuffered: false });
  } else {
    playBufferedTurnAudio(turnID);
  }

  state.awakeUntilMs = 0;
  state.manualArmUntilMs = 0;

  if (state.micActive && state.handsFree) {
    setPresence("connected", "Samantha", "");
  } else if (state.micActive) {
    setPresence("listening", "I’m listening.", "");
  } else if (state.connected) {
    setPresence("connected", "Samantha", "Ready.");
  }
  logEvent(`assistant turn ended: ${reason}`);
}

function playBufferedTurnAudio(turnID) {
  const chunks = state.audioByTurn.get(turnID);
  state.audioByTurn.delete(turnID);
  if (!chunks || chunks.length === 0) {
    return;
  }
  const formatHint = state.audioFormatByTurn.get(turnID) || "";
  state.audioFormatByTurn.delete(turnID);
  const total = chunks.reduce((n, c) => n + c.length, 0);
  const merged = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    merged.set(chunk, offset);
    offset += chunk.length;
  }
  enqueueAudioPlayback(merged, formatHint);
}

function enqueueAudioPlayback(bytes, formatHint) {
  const token = state.playbackToken;
  state.audioQueue = state.audioQueue
    .then(() => playAudioBytes(bytes, formatHint, token))
    .catch((err) => {
      logError(`audio playback error: ${stringifyError(err)}`);
    });
}

function mimeForAudioHint(hint) {
  const h = String(hint || "").trim().toLowerCase();
  if (!h) {
    return "audio/mpeg";
  }
  // Already a content-type (e.g. "audio/wav; charset=binary").
  if (h.includes("/")) {
    return h.split(";")[0].trim();
  }
  if (h.includes("wav")) {
    return "audio/wav";
  }
  if (h.includes("ogg")) {
    return "audio/ogg";
  }
  if (h.includes("mp3") || h.includes("mpeg")) {
    return "audio/mpeg";
  }
  return "audio/mpeg";
}

async function playAudioBytes(bytes, formatHint, token) {
  if (token !== state.playbackToken) {
    return;
  }
  const blob = new Blob([bytes], { type: mimeForAudioHint(formatHint) });
  const url = URL.createObjectURL(blob);
  try {
    const audio = new Audio(url);
    state.currentAudio = audio;
    try {
      await audio.play();
    } catch (err) {
      // Autoplay policies vary; guide the user instead of silently failing.
      setCaption("Tap once to enable audio.", "Then try again.", { clearAfterMs: 3200 });
      logError(`audio play blocked: ${stringifyError(err)}`);
      return;
    }
    await new Promise((resolve) => {
      let done = false;
      const finish = () => {
        if (done) {
          return;
        }
        done = true;
        audio.onended = null;
        audio.onerror = null;
        audio.onpause = null;
        resolve();
      };
      audio.onended = finish;
      audio.onerror = finish;
      // `pause()` (barge-in / Esc) should also unblock the queue.
      audio.onpause = finish;
    });
  } finally {
    if (state.currentAudio && state.currentAudio.src === url) {
      state.currentAudio = null;
    }
    URL.revokeObjectURL(url);
  }
}

function setPresence(mode, primary, secondary) {
  const normalized = mode || "disconnected";
  state.presence = normalized;

  el.pulse.classList.remove(
    "state-disconnected",
    "state-connected",
    "state-listening",
    "state-thinking",
    "state-speaking",
    "state-error",
  );
  el.pulse.classList.add(`state-${normalized}`);

  const primaryLine = primary ?? presencePrimary(normalized);
  const secondaryLine = secondary ?? "";
  setCaption(primaryLine, secondaryLine, { clearAfterMs: normalized === "connected" ? 2600 : 0 });
}

function setCaption(primary, secondary, { clearAfterMs }) {
  if (state.captionClearTimer) {
    clearTimeout(state.captionClearTimer);
    state.captionClearTimer = null;
  }
  if (typeof primary === "string") {
    el.captionPrimary.textContent = primary;
  }
  if (typeof secondary === "string") {
    el.captionSecondary.textContent = secondary;
  }
  if (clearAfterMs && clearAfterMs > 0) {
    state.captionClearTimer = setTimeout(() => {
      if (state.presence === "connected") {
        el.captionPrimary.textContent = "";
        el.captionSecondary.textContent = "";
        updateCaptionsActive();
      }
      state.captionClearTimer = null;
    }, clearAfterMs);
  }
  updateCaptionsActive();
}

function updateCaptionsActive() {
  if (!el.captions) {
    return;
  }
  const primary = (el.captionPrimary.textContent || "").trim();
  const secondary = (el.captionSecondary.textContent || "").trim();
  el.captions.classList.toggle("is-active", Boolean(primary || secondary));
}

function presencePrimary(mode) {
  const lines = {
    disconnected: "Samantha",
    connected: "Samantha",
    listening: "I’m listening.",
    thinking: "Thinking…",
    speaking: "…",
    error: "Something went wrong.",
  };
  return lines[mode] || lines.connected;
}

function logEvent(text) {
  pushEvent(text, "event");
}

function logError(text) {
  pushEvent(text, "error");
}

function pushEvent(text, kind) {
  if (!el.events) {
    return;
  }
  const row = document.createElement("div");
  row.className = "event";
  if (kind === "error") {
    row.style.borderColor = "rgba(255, 99, 92, 0.32)";
    row.style.color = "rgba(255, 214, 212, 0.9)";
  }
  row.textContent = `${new Date().toLocaleTimeString()}  ${text}`;
  el.events.prepend(row);
  while (el.events.children.length > 80) {
    el.events.removeChild(el.events.lastChild);
  }
}

function computeRMS(float32) {
  if (!float32 || float32.length === 0) {
    return 0;
  }
  let sum = 0;
  for (let i = 0; i < float32.length; i += 1) {
    const v = float32[i];
    sum += v * v;
  }
  return Math.sqrt(sum / float32.length);
}

function downsampleFloat32(buffer, inputRate, outputRate) {
  if (outputRate === inputRate) {
    return buffer;
  }
  if (outputRate > inputRate) {
    return buffer;
  }
  const ratio = inputRate / outputRate;
  const newLength = Math.round(buffer.length / ratio);
  const result = new Float32Array(newLength);
  let offsetResult = 0;
  let offsetBuffer = 0;
  while (offsetResult < result.length) {
    const nextOffsetBuffer = Math.round((offsetResult + 1) * ratio);
    let accum = 0;
    let count = 0;
    for (let i = offsetBuffer; i < nextOffsetBuffer && i < buffer.length; i += 1) {
      accum += buffer[i];
      count += 1;
    }
    result[offsetResult] = count > 0 ? accum / count : 0;
    offsetResult += 1;
    offsetBuffer = nextOffsetBuffer;
  }
  return result;
}

function float32ToPCM16Bytes(float32) {
  if (!float32 || float32.length === 0) {
    return new Uint8Array(0);
  }
  const out = new Uint8Array(float32.length * 2);
  for (let i = 0; i < float32.length; i += 1) {
    let s = Math.max(-1, Math.min(1, float32[i]));
    s = s < 0 ? s * 0x8000 : s * 0x7fff;
    const v = Math.round(s);
    out[i * 2] = v & 0xff;
    out[i * 2 + 1] = (v >> 8) & 0xff;
  }
  return out;
}

function bytesToBase64(bytes) {
  let bin = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    const slice = bytes.subarray(i, i + chunk);
    bin += String.fromCharCode(...slice);
  }
  return btoa(bin);
}

function base64ToBytes(base64) {
  if (!base64) {
    return new Uint8Array(0);
  }
  const bin = atob(base64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i += 1) {
    out[i] = bin.charCodeAt(i);
  }
  return out;
}

function softTruncate(text, maxChars) {
  const s = (text || "").trim();
  if (s.length <= maxChars) {
    return s;
  }
  const cut = s.slice(0, maxChars);
  const lastSpace = cut.lastIndexOf(" ");
  if (lastSpace > maxChars * 0.7) {
    return `${cut.slice(0, lastSpace)}…`;
  }
  return `${cut}…`;
}

function clamp01(n) {
  return Math.max(0, Math.min(1, n));
}

function clamp(n, a, b) {
  return Math.max(a, Math.min(b, n));
}

function lerp(a, b, t) {
  return a + (b - a) * t;
}

function stringifyError(err) {
  if (!err) {
    return "unknown error";
  }
  if (typeof err === "string") {
    return err;
  }
  if (err.message) {
    return err.message;
  }
  try {
    return JSON.stringify(err);
  } catch (_jsonErr) {
    return String(err);
  }
}

init();
