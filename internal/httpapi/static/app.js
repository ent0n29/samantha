const MIC_TARGET_SAMPLE_RATE = 16000;
const MIC_PROCESSOR_BUFFER = 2048;
const MIC_WORKLET_MODULE = "mic-worklet.js";
const MIC_TX_TARGET_MS = 45;
const MIC_TX_MAX_LATENCY_MS = 60;
const MIC_TX_TARGET_BYTES = Math.floor(MIC_TARGET_SAMPLE_RATE * 2 * (MIC_TX_TARGET_MS / 1000));
const PERF_POLL_INTERVAL_MS = 2500;
const BARGE_PREROLL_MS = 650;
const BARGE_PREROLL_MAX_BYTES = Math.floor(MIC_TARGET_SAMPLE_RATE * 2 * (BARGE_PREROLL_MS / 1000));
const BARGE_RMS_THRESHOLD = 0.02;
const BARGE_FRAMES_THRESHOLD = 2;
const BARGE_COOLDOWN_MS = 700;

const TASK_BOOTSTRAP_LIMIT = 50;
const TASK_EVENTS_LIMIT = 100;
const TASK_EVENT_DEDUP_MAX = 800;
const TASK_RESYNC_FALLBACK_MS = 1400;
const TASK_REFRESH_AFTER_CONTROL_MS = 1200;

// Lightweight client VAD for "always-on mic" without buffering silence forever.
// This is intentionally heuristic (no ML): dynamic threshold + impulse/noise rejection.
const VAD_PREROLL_MS = 360;
const VAD_PREROLL_MAX_BYTES = Math.floor(MIC_TARGET_SAMPLE_RATE * 2 * (VAD_PREROLL_MS / 1000));
const VAD_MIN_RMS = 0.008;
const VAD_RELATIVE_THRESHOLD = 2.4;
const VAD_MAX_ZCR = 0.25; // zero-crossing rate (0..1). Higher tends to be noise/clicks.
const VAD_MAX_CREST = 25; // peak/rms. Very high tends to be impulse noise (keyboard clicks).
const VAD_ATTACK_FRAMES = 3; // ~190-260ms depending capture frame size; prevents click triggers.
const VAD_SHORT_UTTERANCE_MS = 1500;
const VAD_AUTO_COMMIT_COOLDOWN_MS = 700;
const VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_DEFAULT = 650;
const VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_MIN = 0;
const VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_MAX = 4000;
const VAD_AUTO_COMMIT_GRACE_MS_DEFAULT = 220;
const VAD_AUTO_COMMIT_GRACE_MS_MIN = 0;
const VAD_AUTO_COMMIT_GRACE_MS_MAX = 2000;
const VAD_LONG_UTTERANCE_MS = 3200;
const VAD_SHORT_COMMAND_MS = 900;
const VAD_CONTINUATION_EXTRA_RELEASE_FRAMES = 2;
const VAD_LONG_UTTERANCE_EXTRA_RELEASE_FRAMES = 2;
const VAD_SHORT_COMMAND_RELEASE_DELTA = -1;
const VAD_CONTINUATION_EXTRA_GRACE_MS = 120;
const VAD_SEMANTIC_HINT_STALE_MS = 2400;
const VAD_SEMANTIC_HOLD_MS_MAX = 900;
const VAD_SEMANTIC_HOLD_MS_MIN = 0;
const VAD_SEMANTIC_RELEASE_FRAME_MS = 140;
const VAD_SEMANTIC_COMMIT_RELEASE_DELTA = -2;
const VAD_PROFILE_DEFAULT = "default";
const VAD_TUNING_DEFAULT = {
  releaseFrames: 9,
  releaseFramesShort: 7,
  autoCommitSilenceMs: 620,
  autoCommitSilenceShortMs: 500,
};
const VAD_TUNING_PATIENT = {
  releaseFrames: 11,
  releaseFramesShort: 9,
  autoCommitSilenceMs: 860,
  autoCommitSilenceShortMs: 700,
};
const VAD_TUNING_SNAPPY = {
  releaseFrames: 5,
  releaseFramesShort: 3,
  autoCommitSilenceMs: 280,
  autoCommitSilenceShortMs: 200,
};
const AUDIO_SEGMENT_OVERLAP_MS_DEFAULT = 22;
const AUDIO_SEGMENT_OVERLAP_MS_MIN = 0;
const AUDIO_SEGMENT_OVERLAP_MS_MAX = 150;
const PLAYBACK_PREWARM_COOLDOWN_MS = 45_000;
const PLAYBACK_PREWARM_DURATION_SEC = 0.03;
const UI_SILENCE_BREAKER_MODE_DEFAULT = "visual";
const UI_SILENCE_BREAKER_DELAY_MS_DEFAULT = 750;
const UI_SILENCE_BREAKER_DELAY_MIN_MS = 120;
const UI_SILENCE_BREAKER_DELAY_MAX_MS = 10_000;
const THINKING_CUE_MIN_INTERVAL_MS = 2600;
const THINKING_CUE_DURATION_SEC = 0.12;
const THINKING_CUE_GAIN = 0.045;
const THINKING_CUE_FREQ_START_HZ = 690;
const THINKING_CUE_FREQ_END_HZ = 560;
const HANDSFREE_AWAKE_WINDOW_MS = 30_000;
const FILLER_MODE_DEFAULT = "adaptive";
const FILLER_MODE_ALLOWED = new Set(["off", "adaptive", "occasional", "always"]);
const FILLER_MIN_DELAY_MS_DEFAULT = 1200;
const FILLER_MIN_DELAY_MS_MIN = 0;
const FILLER_MIN_DELAY_MS_MAX = 60_000;
const FILLER_COOLDOWN_MS_DEFAULT = 18_000;
const FILLER_COOLDOWN_MS_MIN = 0;
const FILLER_COOLDOWN_MS_MAX = 180_000;
const FILLER_MAX_PER_TURN_DEFAULT = 1;
const FILLER_MAX_PER_TURN_MIN = 0;
const FILLER_MAX_PER_TURN_MAX = 10;
const FILLER_PHRASES = [
  "Let me think for a moment.",
  "I’m on it.",
  "Thinking through that now.",
  "Got it, working on it.",
];

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
  workletNode: null,
  silentGain: null,
  micCaptureBackend: "none",
  preferAudioWorklet: true,
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
  playbackPrewarmAtMs: 0,
  streamScheduleQueue: Promise.resolve(),
  ignoredTurns: new Set(),

  presence: "disconnected",
  micEnergyTarget: 0,
  speakEnergyTarget: 0,
  energy: 0,

  reconnectTimer: null,
  reconnectInFlight: false,
  reconnectBackoffMs: 250,
  allowReconnect: true,
  lastTurnID: "",

  captionClearTimer: null,
  silenceBreakerTimer: null,
  thinkingCueTimer: null,
  fillerTimer: null,
  lastThinkingCueAtMs: 0,
  awaitingAssistantResponse: false,
  lastCommitAtMs: 0,
  lastFillerAtMs: 0,
  fillerCountForTurn: 0,
  lastFillerPhrase: "",

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
  micTxChunks: [],
  micTxBytes: 0,
  micTxFirstTSMs: 0,

  // Simple client-side VAD to trigger server-side commit without requiring a manual stop.
  lastVoiceAtMs: 0,
  sawSpeech: false,
  lastAutoCommitAtMs: 0,
  vadNoiseRMS: 0,
  vadSpeechFrames: 0,
  vadSilenceFrames: 0,
  vadStreaming: false,
  vadSpeechStartMs: 0,
  vadPreroll: [],
  vadPrerollBytes: 0,
  vadTuning: VAD_TUNING_DEFAULT,
  lastPartialText: "",
  utteranceHadContinuationHold: false,
  semanticHint: {
    reason: "",
    confidence: 0,
    holdMs: 0,
    shouldCommit: false,
    atMs: 0,
  },

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

  perfPollTimer: null,
  perfSnapshot: null,
  perfError: "",
  perfUpdatedAtMs: 0,
  uiSettings: {
    silenceBreakerMode: UI_SILENCE_BREAKER_MODE_DEFAULT,
    silenceBreakerDelayMs: UI_SILENCE_BREAKER_DELAY_MS_DEFAULT,
    taskDeskDefault: false,
    vadProfile: VAD_PROFILE_DEFAULT,
    vadMinUtteranceMs: VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_DEFAULT,
    vadGraceMs: VAD_AUTO_COMMIT_GRACE_MS_DEFAULT,
    audioOverlapMs: AUDIO_SEGMENT_OVERLAP_MS_DEFAULT,
    localSttProfile: "balanced",
    fillerMode: FILLER_MODE_DEFAULT,
    fillerMinDelayMs: FILLER_MIN_DELAY_MS_DEFAULT,
    fillerCooldownMs: FILLER_COOLDOWN_MS_DEFAULT,
    fillerMaxPerTurn: FILLER_MAX_PER_TURN_DEFAULT,
  },

  taskDesk: {
    runtimeEnabled: null,
    syncState: "idle", // idle|snapshot_received|hydrating|synced|degraded
    lastSyncAtMs: 0,
    sessionId: "",
    tasksById: new Map(),
    timelinesByTask: new Map(),
    orderedActive: [],
    orderedAwaiting: [],
    orderedPlanned: [],
    eventSignatures: new Set(),
    eventOrder: [],
    actionInFlight: new Map(),
    bootstrapPromise: null,
    bootstrapForSession: "",
    snapshotSeen: false,
    fallbackTimer: null,
    metrics: {
      task_snapshot_received: 0,
      task_bootstrap_success: 0,
      task_bootstrap_error: 0,
      task_event_dedup_hit: 0,
    },
  },
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
  perfLatency: document.getElementById("perfLatency"),
  perfUpdated: document.getElementById("perfUpdated"),
  events: document.getElementById("events"),
  taskDesk: document.getElementById("taskDesk"),
  taskSync: document.getElementById("taskSync"),
  taskListActive: document.getElementById("taskListActive"),
  taskListAwaiting: document.getElementById("taskListAwaiting"),
  taskListPlanned: document.getElementById("taskListPlanned"),
  taskCountActive: document.getElementById("taskCountActive"),
  taskCountAwaiting: document.getElementById("taskCountAwaiting"),
  taskCountPlanned: document.getElementById("taskCountPlanned"),

  onboard: document.getElementById("onboard"),
  onboardClose: document.getElementById("onboardClose"),
  onboardChecks: document.getElementById("onboardChecks"),
  onboardHandsFree: document.getElementById("onboardHandsFree"),
  onboardMicBtn: document.getElementById("onboardMicBtn"),
  onboardHearBtn: document.getElementById("onboardHearBtn"),
  onboardSettingsBtn: document.getElementById("onboardSettingsBtn"),
};

function init() {
  applySurfaceFlags();
  hydratePrefs();
  applyVADTuning();
  wireUI();
  wireOnboarding();
  void loadUISettings();
  wirePointerParallax();
  initViz();
  startEnergyLoop();
  renderPerfLatency();
  renderTaskDesk();

  const onboarded = localStorage.getItem("samantha.onboarded") === "1";
  setPresence("disconnected", "Samantha", onboarded ? "Connecting…" : "Welcome. Hold anywhere for settings.");
  void ensureSessionConnected();
  void initOnboarding();
}

function applySurfaceFlags() {
  if (!document || !document.body) {
    return;
  }
  document.body.classList.toggle("show-task-desk", shouldShowTaskDesk());
}

function isDebugUI() {
  const qs = new URLSearchParams(window.location.search || "");
  if (qs.get("debug") === "1") {
    return true;
  }
  if (qs.get("debug") === "0") {
    return false;
  }
  return localStorage.getItem("samantha.debug") === "1";
}

function shouldShowTaskDesk() {
  if (!isDebugUI()) {
    return false;
  }
  const qs = new URLSearchParams(window.location.search || "");
  if (qs.get("taskdesk") === "1" || qs.get("tasks") === "1") {
    return true;
  }
  if (qs.get("taskdesk") === "0" || qs.get("tasks") === "0") {
    return false;
  }
  if (!state.taskDesk.runtimeEnabled) {
    return false;
  }
  return Boolean(state.uiSettings.taskDeskDefault);
}

function isTaskDeskVisible() {
  return Boolean(el.taskDesk) && shouldShowTaskDesk();
}

function normalizeSilenceBreakerMode(raw) {
  const mode = String(raw || "").trim().toLowerCase();
  if (mode === "off" || mode === "visual" || mode === "speech") {
    return mode;
  }
  return UI_SILENCE_BREAKER_MODE_DEFAULT;
}

function normalizeSilenceBreakerDelayMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return UI_SILENCE_BREAKER_DELAY_MS_DEFAULT;
  }
  return Math.min(UI_SILENCE_BREAKER_DELAY_MAX_MS, Math.max(UI_SILENCE_BREAKER_DELAY_MIN_MS, Math.round(ms)));
}

function normalizeVADProfile(raw) {
  const profile = String(raw || "").trim().toLowerCase();
  if (profile === "default" || profile === "patient" || profile === "snappy") {
    return profile;
  }
  return VAD_PROFILE_DEFAULT;
}

function normalizeVADMinUtteranceMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_DEFAULT;
  }
  return Math.min(VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_MAX, Math.max(VAD_AUTO_COMMIT_MIN_UTTERANCE_MS_MIN, Math.round(ms)));
}

function normalizeVADGraceMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return VAD_AUTO_COMMIT_GRACE_MS_DEFAULT;
  }
  return Math.min(VAD_AUTO_COMMIT_GRACE_MS_MAX, Math.max(VAD_AUTO_COMMIT_GRACE_MS_MIN, Math.round(ms)));
}

function normalizeAudioOverlapMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return AUDIO_SEGMENT_OVERLAP_MS_DEFAULT;
  }
  return Math.min(AUDIO_SEGMENT_OVERLAP_MS_MAX, Math.max(AUDIO_SEGMENT_OVERLAP_MS_MIN, Math.round(ms)));
}

function normalizeFillerMode(raw) {
  const mode = String(raw || "").trim().toLowerCase();
  if (FILLER_MODE_ALLOWED.has(mode)) {
    return mode;
  }
  return FILLER_MODE_DEFAULT;
}

function normalizeFillerDelayMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return FILLER_MIN_DELAY_MS_DEFAULT;
  }
  return Math.min(FILLER_MIN_DELAY_MS_MAX, Math.max(FILLER_MIN_DELAY_MS_MIN, Math.round(ms)));
}

function normalizeFillerCooldownMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return FILLER_COOLDOWN_MS_DEFAULT;
  }
  return Math.min(FILLER_COOLDOWN_MS_MAX, Math.max(FILLER_COOLDOWN_MS_MIN, Math.round(ms)));
}

function normalizeFillerMaxPerTurn(raw) {
  const n = Number(raw);
  if (!Number.isFinite(n)) {
    return FILLER_MAX_PER_TURN_DEFAULT;
  }
  return Math.min(FILLER_MAX_PER_TURN_MAX, Math.max(FILLER_MAX_PER_TURN_MIN, Math.round(n)));
}

function applyVADTuning() {
  const profile = normalizeVADProfile(state.uiSettings.vadProfile);
  state.uiSettings.vadProfile = profile;
  if (profile === "patient") {
    state.vadTuning = VAD_TUNING_PATIENT;
    return;
  }
  state.vadTuning = profile === "default" ? VAD_TUNING_DEFAULT : VAD_TUNING_SNAPPY;
}

async function loadUISettings() {
  try {
    const res = await fetch("/v1/ui/settings", { cache: "no-store" });
    if (!res.ok) {
      return;
    }
    const payload = await res.json();
    if (Object.prototype.hasOwnProperty.call(payload, "ui_audio_worklet")) {
      state.preferAudioWorklet = Boolean(payload.ui_audio_worklet);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "task_runtime_enabled")) {
      state.taskDesk.runtimeEnabled = Boolean(payload.task_runtime_enabled);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "task_desk_default")) {
      state.uiSettings.taskDeskDefault = Boolean(payload.task_desk_default);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "silence_breaker_mode")) {
      state.uiSettings.silenceBreakerMode = normalizeSilenceBreakerMode(payload.silence_breaker_mode);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "silence_breaker_delay_ms")) {
      state.uiSettings.silenceBreakerDelayMs = normalizeSilenceBreakerDelayMs(payload.silence_breaker_delay_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "vad_profile")) {
      state.uiSettings.vadProfile = normalizeVADProfile(payload.vad_profile);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "vad_min_utterance_ms")) {
      state.uiSettings.vadMinUtteranceMs = normalizeVADMinUtteranceMs(payload.vad_min_utterance_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "vad_grace_ms")) {
      state.uiSettings.vadGraceMs = normalizeVADGraceMs(payload.vad_grace_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "audio_overlap_ms")) {
      state.uiSettings.audioOverlapMs = normalizeAudioOverlapMs(payload.audio_overlap_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "local_stt_profile")) {
      const profile = String(payload.local_stt_profile || "").trim().toLowerCase();
      if (profile === "fast" || profile === "balanced" || profile === "accurate") {
        state.uiSettings.localSttProfile = profile;
      }
    }
    if (Object.prototype.hasOwnProperty.call(payload, "filler_mode")) {
      state.uiSettings.fillerMode = normalizeFillerMode(payload.filler_mode);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "filler_min_delay_ms")) {
      state.uiSettings.fillerMinDelayMs = normalizeFillerDelayMs(payload.filler_min_delay_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "filler_cooldown_ms")) {
      state.uiSettings.fillerCooldownMs = normalizeFillerCooldownMs(payload.filler_cooldown_ms);
    }
    if (Object.prototype.hasOwnProperty.call(payload, "filler_max_per_turn")) {
      state.uiSettings.fillerMaxPerTurn = normalizeFillerMaxPerTurn(payload.filler_max_per_turn);
    }
    applyVADTuning();
    applySurfaceFlags();
    renderTaskDesk();
  } catch (_err) {
    // ignore
  }
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
  el.taskDesk?.addEventListener("click", (evt) => {
    const btn = evt.target instanceof Element ? evt.target.closest("[data-task-action]") : null;
    if (!btn) {
      return;
    }
    const action = String(btn.getAttribute("data-task-action") || "").trim();
    const taskID = String(btn.getAttribute("data-task-id") || "").trim();
    if (!action || !taskID) {
      return;
    }
    void handleTaskDeskAction(taskID, action);
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
      sendControl("interrupt", { reason: "manual_interrupt" });
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
      return;
    }
    if (evt.key === "y" || evt.key === "Y") {
      if (!isTaskDeskVisible()) {
        return;
      }
      evt.preventDefault();
      sendControl("approve_task_step");
      setCaption("Approval sent.", "", { clearAfterMs: 1400 });
      return;
    }
    if (evt.key === "n" || evt.key === "N") {
      if (!isTaskDeskVisible()) {
        return;
      }
      evt.preventDefault();
      sendControl("deny_task_step");
      setCaption("Denial sent.", "", { clearAfterMs: 1400 });
      return;
    }
    if (evt.key === "x" || evt.key === "X") {
      if (!isTaskDeskVisible()) {
        return;
      }
      evt.preventDefault();
      sendControl("cancel_task");
      setCaption("Cancel sent.", "", { clearAfterMs: 1400 });
    }
  });

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      stopPerfPolling();
      return;
    }
    if (el.debug && !el.debug.classList.contains("hidden")) {
      startPerfPolling();
    }
    if (state.allowReconnect && !state.connected) {
      void reconnectFlow();
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
        setCaption("Microphone enabled.", "Press Space to toggle mic.", { clearAfterMs: 2600 });
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
      if (Object.prototype.hasOwnProperty.call(state.onboardingStatus, "ui_audio_worklet")) {
        state.preferAudioWorklet = Boolean(state.onboardingStatus.ui_audio_worklet);
      }
      if (Object.prototype.hasOwnProperty.call(state.onboardingStatus, "task_runtime_enabled")) {
        state.taskDesk.runtimeEnabled = Boolean(state.onboardingStatus.task_runtime_enabled);
      }
    }
  } catch (_err) {
    // ignore
  }

  renderOnboarding();
  renderTaskDesk();

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
  rows.push({
    id: "mic_pipeline",
    status: state.preferAudioWorklet ? "ok" : "warn",
    label: "Mic pipeline",
    detail: state.preferAudioWorklet ? "AudioWorklet + fallback" : "legacy ScriptProcessor forced",
    fix: state.preferAudioWorklet ? "" : "Set APP_UI_AUDIO_WORKLET=true to enable low-latency capture attempts.",
  });

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
  const handsFreeRaw = localStorage.getItem("samantha.handsFree");
  const savedHandsFree = handsFreeRaw === null ? true : handsFreeRaw === "1";
  if (handsFreeRaw === null) {
    localStorage.setItem("samantha.handsFree", "1");
  }
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

function normalizePartialForEndpointing(raw) {
  return String(raw || "")
    .replace(/\s+/g, " ")
    .trim()
    .toLowerCase();
}

function hasContinuationCue(partialText) {
  const t = normalizePartialForEndpointing(partialText);
  if (!t) {
    return false;
  }
  if (/[,:;…-]$/.test(t)) {
    return true;
  }
  if (/\b(and|but|because|so|then|which|that|if|when|while)\b\s*$/.test(t)) {
    return true;
  }
  if (/\b(i mean|for example|for instance|in order to)\b\s*$/.test(t)) {
    return true;
  }
  return false;
}

function hasStrongStopCue(partialText) {
  const t = normalizePartialForEndpointing(partialText);
  if (!t) {
    return false;
  }
  if (/[.!?]["']?$/.test(t)) {
    return true;
  }
  if (/\b(done|stop|thanks|thank you)\b$/.test(t)) {
    return true;
  }
  return false;
}

function normalizeSemanticHintHoldMs(raw) {
  const ms = Number(raw);
  if (!Number.isFinite(ms)) {
    return 0;
  }
  return Math.min(VAD_SEMANTIC_HOLD_MS_MAX, Math.max(VAD_SEMANTIC_HOLD_MS_MIN, Math.round(ms)));
}

function activeSemanticHint() {
  const hint = state.semanticHint;
  if (!hint || !hint.atMs) {
    return null;
  }
  if (Date.now() - hint.atMs > VAD_SEMANTIC_HINT_STALE_MS) {
    return null;
  }
  return hint;
}

function resetSemanticHint() {
  state.semanticHint.reason = "";
  state.semanticHint.confidence = 0;
  state.semanticHint.holdMs = 0;
  state.semanticHint.shouldCommit = false;
  state.semanticHint.atMs = 0;
}

function adaptiveReleaseFrames(baseRelease, utteranceMs, partialText) {
  let release = Number(baseRelease) || 1;
  let continuationHold = false;
  if (hasContinuationCue(partialText)) {
    release += VAD_CONTINUATION_EXTRA_RELEASE_FRAMES;
    continuationHold = true;
  }
  if (utteranceMs > VAD_LONG_UTTERANCE_MS) {
    release += VAD_LONG_UTTERANCE_EXTRA_RELEASE_FRAMES;
  }
  if (utteranceMs > 0 && utteranceMs < VAD_SHORT_COMMAND_MS && hasStrongStopCue(partialText)) {
    release += VAD_SHORT_COMMAND_RELEASE_DELTA;
  }
  const semantic = activeSemanticHint();
  if (semantic) {
    const releaseDelta = Math.round(normalizeSemanticHintHoldMs(semantic.holdMs) / VAD_SEMANTIC_RELEASE_FRAME_MS);
    release += releaseDelta;
    if (semantic.shouldCommit) {
      release += VAD_SEMANTIC_COMMIT_RELEASE_DELTA;
    }
  }
  const minRelease = Math.max(1, Math.round((Number(baseRelease) || 1) - 1));
  const maxRelease = Math.max(minRelease, Math.round((Number(baseRelease) || 1) + 5));
  release = Math.max(minRelease, Math.min(maxRelease, Math.round(release)));
  return { release, continuationHold };
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
  const utteranceMs = state.vadSpeechStartMs > 0 ? now - state.vadSpeechStartMs : 0;
  const minUtteranceMs = normalizeVADMinUtteranceMs(state.uiSettings?.vadMinUtteranceMs);
  if (utteranceMs > 0 && utteranceMs < minUtteranceMs) {
    return;
  }
  const tuning = state.vadTuning || VAD_TUNING_SNAPPY;
  let silenceTargetMs =
    utteranceMs > 0 && utteranceMs < VAD_SHORT_UTTERANCE_MS ? tuning.autoCommitSilenceShortMs : tuning.autoCommitSilenceMs;
  // Keep auto-commit as a conservative fallback; frame-based release is the primary trigger.
  silenceTargetMs += normalizeVADGraceMs(state.uiSettings?.vadGraceMs);
  if (hasContinuationCue(state.lastPartialText)) {
    silenceTargetMs += VAD_CONTINUATION_EXTRA_GRACE_MS;
  }
  const semantic = activeSemanticHint();
  if (semantic) {
    silenceTargetMs += normalizeSemanticHintHoldMs(semantic.holdMs);
    if (semantic.shouldCommit) {
      silenceTargetMs = Math.min(silenceTargetMs, 260);
    }
  }
  // Tune for "talk, then pause": commit after a short silence.
  const silenceMs = now - state.lastVoiceAtMs;
  if (silenceMs < silenceTargetMs) {
    return;
  }
  // Avoid spamming commit during long silences.
  if (now - state.lastAutoCommitAtMs < VAD_AUTO_COMMIT_COOLDOWN_MS) {
    return;
  }

  state.lastAutoCommitAtMs = now;
  state.sawSpeech = false;
  state.vadStreaming = false;
  state.vadSpeechFrames = 0;
  state.vadSilenceFrames = 0;
  state.vadSpeechStartMs = 0;
  state.utteranceHadContinuationHold = false;
  resetSemanticHint();
  clearVADPreroll();
  sendControl("stop", { reason: "silence_fallback" });
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
  resetTaskDeskState("");
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
  resetTaskDeskState(state.sessionId);
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

  state.allowReconnect = true;
  setPresence("connected", "Samantha", "Connecting…");

  const ws = new WebSocket(wsURL);
  state.ws = ws;

  ws.addEventListener("open", () => {
    state.connected = true;
    state.reconnectBackoffMs = 250;
    markTaskDeskConnected();
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
    markTaskDeskDisconnected();
    void stopMic({ sendStop: false });
    stopPlayback({ clearBuffered: true });
    state.bargeInActive = false;
    state.bargeInFrames = 0;
    clearBargePreroll();
    if (state.allowReconnect) {
      if (document.hidden) {
        setPresence("connected", "Samantha", "Reconnecting when visible…");
      } else {
        setPresence("connected", "Samantha", "Reconnecting…");
        scheduleReconnect();
      }
    }
    logEvent("ws disconnected");
  });

  ws.addEventListener("error", () => {
    // Close event will follow; keep it quiet.
  });

  await waitForWSOpen(ws, 8000);
}

function scheduleReconnect() {
  if (!state.allowReconnect) {
    return;
  }
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
  if (state.reconnectInFlight) {
    return;
  }
  if (!state.allowReconnect) {
    return;
  }
  state.reconnectInFlight = true;
  const priorSessionID = (state.sessionId || "").trim();
  let lastErr = null;
  try {
    if (!state.allowReconnect) {
      return;
    }
    if (!priorSessionID) {
      await createSession();
    }
    if (!state.allowReconnect) {
      return;
    }
    await connect();
  } catch (err) {
    if (!state.allowReconnect) {
      return;
    }
    lastErr = err;
    if (priorSessionID) {
      try {
        if (!state.allowReconnect) {
          return;
        }
        await createSession();
        if (!state.allowReconnect) {
          return;
        }
        await connect();
        return;
      } catch (fallbackErr) {
        // fall through to retry schedule below.
        lastErr = fallbackErr || err;
      }
    }
    if (isTransientReconnectError(lastErr)) {
      setPresence("connected", "Samantha", "Reconnecting…");
    } else {
      setPresence("error", "Samantha", stringifyError(lastErr));
    }
    markTaskDeskSyncState("degraded");
    state.taskDesk.metrics.task_bootstrap_error += 1;
    renderTaskDesk();
    if (state.allowReconnect) {
      scheduleReconnect();
    }
  } finally {
    state.reconnectInFlight = false;
  }
}

function isTransientReconnectError(err) {
  const msg = stringifyError(err).toLowerCase();
  if (!msg) {
    return false;
  }
  return (
    msg.includes("websocket") ||
    msg.includes("networkerror") ||
    msg.includes("network error") ||
    msg.includes("failed to fetch") ||
    msg.includes("timeout") ||
    msg.includes("closed before open")
  );
}

function waitForWSOpen(ws, timeoutMs) {
  return new Promise((resolve, reject) => {
    if (!ws) {
      reject(new Error("websocket unavailable"));
      return;
    }
    if (ws.readyState === WebSocket.OPEN) {
      resolve();
      return;
    }
    if (ws.readyState === WebSocket.CLOSED) {
      reject(new Error("websocket closed"));
      return;
    }

    let settled = false;
    let timer = null;

    const cleanup = () => {
      ws.removeEventListener("open", onOpen);
      ws.removeEventListener("close", onCloseBeforeOpen);
      ws.removeEventListener("error", onErrorBeforeOpen);
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
    };

    const onOpen = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      resolve();
    };

    const onCloseBeforeOpen = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(new Error("websocket closed before open"));
    };

    const onErrorBeforeOpen = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(new Error("websocket connect error"));
    };

    ws.addEventListener("open", onOpen);
    ws.addEventListener("close", onCloseBeforeOpen);
    ws.addEventListener("error", onErrorBeforeOpen);
    timer = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(new Error("websocket open timeout"));
    }, Math.max(500, Number(timeoutMs) || 0));
  });
}

async function disconnect() {
  state.allowReconnect = false;
  if (state.reconnectTimer) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
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
  markTaskDeskDisconnected();
  markTaskDeskSyncState("idle");
  renderTaskDesk();
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
  resetTaskDeskState("");
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
    startPerfPolling();
    logEvent("debug opened");
    return;
  }
  el.debug.classList.add("hidden");
  stopPerfPolling();
  logEvent("debug closed");
}

function startPerfPolling() {
  if (state.perfPollTimer) {
    return;
  }
  void refreshPerfLatency();
  state.perfPollTimer = window.setInterval(() => {
    void refreshPerfLatency();
  }, PERF_POLL_INTERVAL_MS);
}

function stopPerfPolling() {
  if (!state.perfPollTimer) {
    return;
  }
  window.clearInterval(state.perfPollTimer);
  state.perfPollTimer = null;
}

async function refreshPerfLatency() {
  try {
    const res = await fetch("/v1/perf/latency", { cache: "no-store" });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    state.perfSnapshot = await res.json();
    state.perfUpdatedAtMs = Date.now();
    state.perfError = "";
  } catch (err) {
    state.perfError = stringifyError(err);
  }
  renderPerfLatency();
}

function orderedPerfStages(snapshot) {
  const stageOrder = [
    "stop_to_stt_committed",
    "commit_to_tts_ready",
    "commit_to_context_ready",
    "commit_to_brain_first_delta",
    "commit_to_assistant_working",
    "commit_to_thinking_delta",
    "commit_to_first_text",
    "brain_first_delta_to_first_audio",
    "commit_to_first_audio",
    "turn_total",
  ];
  const stageRank = new Map();
  for (let i = 0; i < stageOrder.length; i += 1) {
    stageRank.set(stageOrder[i], i);
  }
  const list = Array.isArray(snapshot?.stages) ? snapshot.stages.slice() : [];
  list.sort((a, b) => {
    const ar = stageRank.has(String(a?.stage || "")) ? stageRank.get(String(a?.stage || "")) : 99;
    const br = stageRank.has(String(b?.stage || "")) ? stageRank.get(String(b?.stage || "")) : 99;
    if (ar !== br) {
      return ar - br;
    }
    return String(a?.stage || "").localeCompare(String(b?.stage || ""));
  });
  return list;
}

function displayStageName(stage) {
  switch (stage) {
    case "stop_to_stt_committed":
      return "stop->stt-committed";
    case "commit_to_tts_ready":
      return "commit->tts-ready";
    case "commit_to_context_ready":
      return "commit->context-ready";
    case "commit_to_brain_first_delta":
      return "commit->brain-first-delta";
    case "commit_to_assistant_working":
      return "commit->assistant-working";
    case "commit_to_thinking_delta":
      return "commit->thinking-delta";
    case "commit_to_first_text":
      return "commit->first-text";
    case "brain_first_delta_to_first_audio":
      return "brain-first-delta->first-audio";
    case "commit_to_first_audio":
      return "commit->first-audio";
    case "turn_total":
      return "turn-total";
    default:
      return String(stage || "unknown");
  }
}

function fmtMS(n) {
  const v = Number(n);
  if (!Number.isFinite(v) || v < 0) {
    return "n/a";
  }
  return `${Math.round(v)}ms`;
}

function playbackOverlapSec() {
  const ms = normalizeAudioOverlapMs(state.uiSettings?.audioOverlapMs);
  if (!Number.isFinite(ms) || ms <= 0) {
    return 0;
  }
  return ms / 1000;
}

function renderPerfLatency() {
  if (!el.perfLatency || !el.perfUpdated) {
    return;
  }
  const root = el.perfLatency;
  root.innerHTML = "";

  if (state.perfError) {
    const row = document.createElement("div");
    row.className = "perf-card is-bad";
    row.textContent = `Latency fetch failed: ${state.perfError}`;
    root.appendChild(row);
    el.perfUpdated.textContent = "error";
    return;
  }

  const snapshot = state.perfSnapshot;
  if (!snapshot || !Array.isArray(snapshot.stages) || snapshot.stages.length === 0) {
    const row = document.createElement("div");
    row.className = "perf-card";
    row.textContent = "Warming up latency window...";
    root.appendChild(row);
    el.perfUpdated.textContent = state.perfUpdatedAtMs ? "just now" : "idle";
    return;
  }

  const stages = orderedPerfStages(snapshot);
  for (const raw of stages) {
    const stage = String(raw?.stage || "");
    const p95 = Number(raw?.p95_ms);
    const target = Number(raw?.target_p95_ms);
    const samples = Number(raw?.samples) || 0;
    const isPassing = Number.isFinite(target) && target > 0 && Number.isFinite(p95) && p95 <= target;

    const card = document.createElement("div");
    card.className = `perf-card ${isPassing ? "is-good" : target > 0 ? "is-bad" : ""}`.trim();

    const title = document.createElement("div");
    title.className = "perf-stage";
    title.textContent = `${displayStageName(stage)} (${samples})`;

    const body = document.createElement("div");
    body.className = "perf-metrics";
    const targetPart = target > 0 ? ` target-p95 ${fmtMS(target)}` : "";
    body.textContent = `p50 ${fmtMS(raw?.p50_ms)}  p95 ${fmtMS(raw?.p95_ms)}  p99 ${fmtMS(raw?.p99_ms)}  avg ${fmtMS(raw?.avg_ms)}${targetPart}`;

    card.appendChild(title);
    card.appendChild(body);
    root.appendChild(card);
  }

  const indicators = Array.isArray(snapshot.indicators) ? snapshot.indicators : [];
  if (indicators.length > 0) {
    const summary = [];
    for (const raw of indicators) {
      const name = String(raw?.name || "").trim();
      const count = Number(raw?.count);
      if (!name || !Number.isFinite(count) || count <= 0) {
        continue;
      }
      summary.push(`${name}=${Math.round(count)}`);
    }
    if (summary.length > 0) {
      const row = document.createElement("div");
      row.className = "perf-card";
      row.textContent = `indicators ${summary.join("  ")}`;
      root.appendChild(row);
    }
  }

  if (state.perfUpdatedAtMs > 0) {
    const agoSec = Math.max(0, Math.round((Date.now() - state.perfUpdatedAtMs) / 100) / 10);
    el.perfUpdated.textContent = `${agoSec}s ago`;
  } else {
    el.perfUpdated.textContent = "idle";
  }
}

function sendJSON(payload) {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    return false;
  }
  try {
    state.ws.send(JSON.stringify(payload));
    return true;
  } catch (err) {
    logError(`ws send failed: ${stringifyError(err)}`);
    return false;
  }
}

function sendControl(action, opts = {}) {
  if (!state.sessionId) {
    return;
  }
  const taskID = typeof opts.taskID === "string" ? opts.taskID.trim() : "";
  const approved = typeof opts.approved === "boolean" ? opts.approved : undefined;
  const scope = typeof opts.scope === "string" ? opts.scope.trim() : "";
  const reason = typeof opts.reason === "string" ? opts.reason.trim().toLowerCase() : "";
  const payload = {
    type: "client_control",
    session_id: state.sessionId,
    action,
  };
  if (action === "stop") {
    payload.ts_ms = Date.now();
  }
  if (taskID) {
    payload.task_id = taskID;
  }
  if (typeof approved === "boolean") {
    payload.approved = approved;
  }
  if (scope) {
    payload.scope = scope;
  }
  if (reason) {
    payload.reason = reason;
  }
  const ok = sendJSON({
    ...payload,
  });
  if (ok) {
    logEvent(`control sent: ${action}`);
  }
  return ok;
}

function mergePCMChunks(chunks, totalBytes) {
  const knownBytes = Number(totalBytes) || 0;
  let size = knownBytes;
  if (size <= 0) {
    size = 0;
    for (const chunk of chunks || []) {
      if (!chunk || chunk.length === 0) {
        continue;
      }
      size += chunk.length;
    }
  }
  if (size <= 0) {
    return new Uint8Array(0);
  }
  const out = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks || []) {
    if (!chunk || chunk.length === 0) {
      continue;
    }
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return offset === size ? out : out.subarray(0, offset);
}

function sendClientAudioBytes(pcmBytes, tsMs) {
  if (!state.sessionId || !pcmBytes || pcmBytes.length === 0) {
    return false;
  }
  return sendJSON({
    type: "client_audio_chunk",
    session_id: state.sessionId,
    seq: ++state.seq,
    pcm16_base64: bytesToBase64(pcmBytes),
    sample_rate: MIC_TARGET_SAMPLE_RATE,
    ts_ms: tsMs || Date.now(),
  });
}

function clearQueuedMicPCM() {
  state.micTxChunks = [];
  state.micTxBytes = 0;
  state.micTxFirstTSMs = 0;
}

function queueMicPCM(pcmBytes, tsMs) {
  if (!pcmBytes || pcmBytes.length === 0) {
    return;
  }
  if (!state.micTxFirstTSMs) {
    state.micTxFirstTSMs = tsMs || Date.now();
  }
  state.micTxChunks.push(pcmBytes);
  state.micTxBytes += pcmBytes.length;
}

function flushQueuedMicPCM(nowMs, force) {
  if (!state.micTxChunks || state.micTxChunks.length === 0 || state.micTxBytes <= 0) {
    return false;
  }
  const now = nowMs || Date.now();
  const firstTs = state.micTxFirstTSMs || now;
  const ageMs = Math.max(0, now - firstTs);
  if (!force && state.micTxBytes < MIC_TX_TARGET_BYTES && ageMs < MIC_TX_MAX_LATENCY_MS) {
    return false;
  }
  const merged = mergePCMChunks(state.micTxChunks, state.micTxBytes);
  clearQueuedMicPCM();
  return sendClientAudioBytes(merged, firstTs);
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
  const bytes = state.bargePrerollBytes;
  clearBargePreroll();
  const merged = mergePCMChunks(chunks, bytes);
  sendClientAudioBytes(merged, Date.now());
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
  const bytes = state.vadPrerollBytes;
  clearVADPreroll();
  const merged = mergePCMChunks(chunks, bytes);
  sendClientAudioBytes(merged, Date.now());
}

function resetVAD() {
  state.lastVoiceAtMs = 0;
  state.sawSpeech = false;
  state.lastAutoCommitAtMs = 0;
  state.vadNoiseRMS = 0;
  state.vadSpeechFrames = 0;
  state.vadSilenceFrames = 0;
  state.vadStreaming = false;
  state.vadSpeechStartMs = 0;
  state.lastPartialText = "";
  state.utteranceHadContinuationHold = false;
  resetSemanticHint();
  clearVADPreroll();
  clearQueuedMicPCM();
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
    prewarmPlaybackPath(state.playbackContext);
    return;
  }
  void ensurePlaybackContext().then((ctx) => {
    prewarmPlaybackPath(ctx);
  }).catch((_err) => {
    // Ignore; playback will guide the user on demand.
  });
}

function prewarmPlaybackPath(ctx) {
  const now = Date.now();
  if (now - state.playbackPrewarmAtMs < PLAYBACK_PREWARM_COOLDOWN_MS) {
    return;
  }
  if (!ctx || ctx.state !== "running" || !state.playbackGain) {
    return;
  }
  if (!ctx.createBuffer || !ctx.createBufferSource || !ctx.createGain) {
    return;
  }

  const frames = Math.max(1, Math.floor(ctx.sampleRate * PLAYBACK_PREWARM_DURATION_SEC));
  const t0 = Math.max(ctx.currentTime, state.playbackNextTime || 0);
  const src = ctx.createBufferSource();
  const gain = ctx.createGain();
  src.buffer = ctx.createBuffer(1, frames, ctx.sampleRate);
  gain.gain.value = 0.0001;
  src.connect(gain);
  gain.connect(state.playbackGain);
  src.onended = () => {
    try {
      src.disconnect();
      gain.disconnect();
    } catch (_err) {
      // ignore
    }
  };
  src.start(t0);
  src.stop(t0 + PLAYBACK_PREWARM_DURATION_SEC);
  state.playbackPrewarmAtMs = now;
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
  let overlap = 0;
  const canOverlap = Number.isFinite(state.playbackNextTime) && state.playbackNextTime > baseStart;
  if (canOverlap) {
    overlap = Math.min(playbackOverlapSec(), buffer.duration * 0.18);
  }
  if (!state.playbackNextTime || state.playbackNextTime < baseStart - 0.1) {
    state.playbackNextTime = baseStart;
  }
  const startAt = Math.max(baseStart, state.playbackNextTime - overlap);
  const endAt = startAt + buffer.duration;

  const src = ctx.createBufferSource();
  src.buffer = buffer;

  const segGain = ctx.createGain();
  const fade = Math.min(0.018, buffer.duration * 0.25);
  const fadeIn = Math.max(0.004, Math.min(fade, overlap || fade));
  const fadeInEnd = Math.min(endAt, startAt + fadeIn);
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
  let overlap = 0;
  const canOverlap = Number.isFinite(state.playbackNextTime) && state.playbackNextTime > baseStart;
  if (canOverlap) {
    overlap = Math.min(playbackOverlapSec(), buffer.duration * 0.18);
  }
  if (!state.playbackNextTime || state.playbackNextTime < baseStart - 0.1) {
    state.playbackNextTime = baseStart;
  }
  const startAt = Math.max(baseStart, state.playbackNextTime - overlap);
  const endAt = startAt + buffer.duration;

  const src = ctx.createBufferSource();
  src.buffer = buffer;

  const segGain = ctx.createGain();
  const fade = Math.min(0.018, buffer.duration * 0.25);
  const fadeIn = Math.max(0.004, Math.min(fade, overlap || fade));
  const fadeInEnd = Math.min(endAt, startAt + fadeIn);
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

function processMicFrame(input, inputSampleRate) {
  if (!state.micActive || !input || input.length === 0) {
    return;
  }
  const metrics = analyzeAudioFrame(input);
  const rms = metrics.rms || 0;
  state.micEnergyTarget = rms * 3.2;

  const downsampled = downsampleFloat32(input, inputSampleRate, MIC_TARGET_SAMPLE_RATE);
  const pcmBytes = float32ToPCM16Bytes(downsampled);
  if (pcmBytes.length === 0) {
    return;
  }

  const speechLike = isSpeechLike(metrics);
  if (!state.vadStreaming && state.presence !== "speaking") {
    updateNoiseFloor(rms, speechLike);
  }

  const now = Date.now();
  const attackFrames = state.handsFree ? VAD_ATTACK_FRAMES : Math.max(1, VAD_ATTACK_FRAMES - 1);

  if (state.presence === "speaking" && !state.bargeInActive) {
    // Flush any pending client-audio batch before we move into half-duplex hold.
    flushQueuedMicPCM(now, true);
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
        state.vadSpeechStartMs = now;
        state.sawSpeech = true;
        state.lastVoiceAtMs = now;
        state.utteranceHadContinuationHold = false;
        clearVADPreroll();

        stopPlayback({ clearBuffered: true });
        sendControl("interrupt", { reason: "barge_interrupt" });
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

  // VAD-gated streaming: only send audio to the backend while we're inside an utterance.
  // This keeps the UX "talk naturally": we auto-commit on a short silence instead of
  // requiring a manual stop, even in push-to-talk mode.
  if (!state.vadStreaming) {
    pushVADPreroll(pcmBytes);
    if (speechLike) {
      state.vadSpeechFrames += 1;
    } else {
      state.vadSpeechFrames = Math.max(0, state.vadSpeechFrames - 1);
    }
    if (state.vadSpeechFrames >= attackFrames) {
      state.vadStreaming = true;
      state.vadSilenceFrames = 0;
      state.vadSpeechStartMs = now;
      state.sawSpeech = true;
      state.lastVoiceAtMs = now;
      state.utteranceHadContinuationHold = false;
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

  queueMicPCM(pcmBytes, now);
  flushQueuedMicPCM(now, false);

  const utteranceMs = state.vadSpeechStartMs > 0 ? now - state.vadSpeechStartMs : 0;
  const tuning = state.vadTuning || VAD_TUNING_SNAPPY;
  const baseRelease = utteranceMs > 0 && utteranceMs < VAD_SHORT_UTTERANCE_MS ? tuning.releaseFramesShort : tuning.releaseFrames;
  const endpoint = adaptiveReleaseFrames(baseRelease, utteranceMs, state.lastPartialText);
  state.utteranceHadContinuationHold = endpoint.continuationHold;
  if (!speechLike && state.vadSilenceFrames >= endpoint.release) {
    flushQueuedMicPCM(now, true);
    state.lastAutoCommitAtMs = now;
    state.sawSpeech = false;
    state.vadStreaming = false;
    state.vadSpeechFrames = 0;
    state.vadSilenceFrames = 0;
    state.vadSpeechStartMs = 0;
    clearVADPreroll();
    resetSemanticHint();
    sendControl("stop", { reason: state.utteranceHadContinuationHold ? "continuation_hold" : "silence_release" });
    state.utteranceHadContinuationHold = false;
  }
}

function connectScriptProcessorCapture(ctx, source, silentGain) {
  const processor = ctx.createScriptProcessor(MIC_PROCESSOR_BUFFER, 1, 1);
  processor.onaudioprocess = (e) => {
    processMicFrame(e.inputBuffer.getChannelData(0), ctx.sampleRate);
  };
  source.connect(processor);
  processor.connect(silentGain);
  silentGain.connect(ctx.destination);
  return processor;
}

function resolveUIAsset(path) {
  const base = window.location.pathname.startsWith("/ui/") ? "/ui/" : "/";
  return new URL(path, `${window.location.origin}${base}`).toString();
}

async function connectAudioWorkletCapture(ctx, source, silentGain) {
  if (!state.preferAudioWorklet) {
    return null;
  }
  if (!ctx.audioWorklet || typeof window.AudioWorkletNode !== "function") {
    return null;
  }
  try {
    await ctx.audioWorklet.addModule(resolveUIAsset(MIC_WORKLET_MODULE));
    const node = new AudioWorkletNode(ctx, "samantha-mic-capture", {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      outputChannelCount: [1],
      channelCount: 1,
      channelCountMode: "explicit",
    });
    node.port.onmessage = (evt) => {
      const data = evt.data || {};
      if (data.type !== "audio-frame" || !(data.samples instanceof ArrayBuffer)) {
        return;
      }
      const sampleRate = Number(data.sampleRate) || ctx.sampleRate;
      processMicFrame(new Float32Array(data.samples), sampleRate);
    };
    source.connect(node);
    node.connect(silentGain);
    silentGain.connect(ctx.destination);
    return node;
  } catch (err) {
    logEvent(`audio worklet unavailable: ${stringifyError(err)}`);
    return null;
  }
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
    const silentGain = ctx.createGain();
    silentGain.gain.value = 0;
    let processor = null;
    const workletNode = await connectAudioWorkletCapture(ctx, source, silentGain);
    if (!workletNode) {
      processor = connectScriptProcessorCapture(ctx, source, silentGain);
    }
    const backend = workletNode ? "audio_worklet" : "script_processor";

    state.audioContext = ctx;
    state.mediaStream = stream;
    state.mediaSource = source;
    state.processor = processor;
    state.workletNode = workletNode;
    state.silentGain = silentGain;
    state.micCaptureBackend = backend;
    state.micActive = true;
    prewarmPlaybackPath(state.playbackContext);
    logEvent(`microphone started (${backend})`);
  } catch (err) {
    setPresence("error", "Samantha", `Mic error: ${stringifyError(err)}`);
    logError(`mic start error: ${stringifyError(err)}`);
  }
}

async function stopMic({ sendStop }) {
  if (!state.micActive && !state.audioContext && !state.mediaStream) {
    return;
  }
  const hadSpeechCandidate = state.vadStreaming || state.sawSpeech || state.vadSpeechFrames > 0 || state.micTxBytes > 0;
  const shouldSendStop = Boolean(sendStop && state.sessionId && hadSpeechCandidate);

  if (shouldSendStop && state.vadPrerollBytes > 0 && (state.vadSpeechFrames > 0 || state.sawSpeech)) {
    // If the user stops the mic mid-utterance before we fully entered streaming mode,
    // flush the preroll so the backend still has something to transcribe/commit.
    flushVADPreroll();
  }
  flushQueuedMicPCM(Date.now(), true);
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
    if (state.workletNode) {
      state.workletNode.port.onmessage = null;
      state.workletNode.disconnect();
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
    state.workletNode = null;
    state.silentGain = null;
    state.micCaptureBackend = "none";
    clearQueuedMicPCM();
  }

  if (state.connected) {
    setPresence("connected", "Samantha", state.handsFree ? "Mic off." : "Ready.");
  } else {
    setPresence("disconnected", "Samantha", "Disconnected.");
  }

  if (shouldSendStop) {
    sendControl("stop", { reason: "manual_stop" });
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
      state.lastPartialText = String(msg.text || "");
      setPresence("listening", "I’m listening.", msg.text || "");
      break;
    case "stt_committed":
      if (!isAwake()) {
        break;
      }
      state.bargeInActive = false;
      state.bargeInFrames = 0;
      clearBargePreroll();
      if (state.handsFree) {
        // Keep the wake window open for follow-ups so the user doesn't need to repeat the wake word
        // after every turn.
        state.awakeUntilMs = Math.max(state.awakeUntilMs, Date.now() + HANDSFREE_AWAKE_WINDOW_MS);
        state.manualArmUntilMs = 0;
      } else {
        state.awakeUntilMs = 0;
        state.manualArmUntilMs = 0;
      }
      state.lastCommitAtMs = Date.now();
      state.fillerCountForTurn = 0;
      state.lastPartialText = "";
      resetSemanticHint();
      state.awaitingAssistantResponse = true;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      setPresence("thinking", "", "");
      setCaption("", msg.text || "", { clearAfterMs: 1200 });
      break;
    case "semantic_endpoint_hint":
      handleSemanticEndpointHint(msg);
      break;
    case "system_event":
      handleSystemEvent(msg);
      break;
    case "assistant_thinking_delta":
      handleAssistantThinkingDelta(msg);
      break;
    case "assistant_text_delta":
      if (state.bargeInActive) {
        break;
      }
      if (state.ignoredTurns && state.ignoredTurns.has(msg.turn_id || "")) {
        break;
      }
      state.awaitingAssistantResponse = false;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      cancelSpokenFiller();
      setPresence("thinking", "", "");
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
      state.awaitingAssistantResponse = false;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      cancelSpokenFiller();
      state.speakEnergyTarget = Math.max(state.speakEnergyTarget, 0.32);
      setPresence("speaking", "…", "");
      handleAssistantAudio(msg);
      break;
    case "assistant_turn_end":
      handleAssistantTurnEnd(msg);
      break;
    case "task_status_snapshot":
      handleTaskSnapshot(msg);
      break;
    case "task_created":
    case "task_plan_graph":
    case "task_plan_delta":
    case "task_step_started":
    case "task_step_log":
    case "task_step_completed":
    case "task_waiting_approval":
    case "task_completed":
    case "task_failed":
      handleTaskEvent(msg);
      break;
    case "error_event":
      state.awaitingAssistantResponse = false;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      cancelSpokenFiller();
      setPresence("error", "Something went wrong.", `${msg.code || "error"} (${msg.source || "gateway"})`);
      logError(`error_event ${msg.code || ""} ${msg.detail || ""}`.trim());
      break;
    default:
      logEvent(`event: ${t}`);
      break;
  }
}

function handleSemanticEndpointHint(msg) {
  const reason = String(msg.reason || "").trim().toLowerCase();
  const holdMs = normalizeSemanticHintHoldMs(msg.hold_ms);
  const confidenceRaw = Number(msg.confidence);
  const confidence = Number.isFinite(confidenceRaw) ? clamp(confidenceRaw, 0, 1) : 0;
  const shouldCommit = Boolean(msg.should_commit);

  state.semanticHint.reason = reason;
  state.semanticHint.holdMs = holdMs;
  state.semanticHint.confidence = confidence;
  state.semanticHint.shouldCommit = shouldCommit;
  state.semanticHint.atMs = Date.now();

  if (!shouldCommit) {
    return;
  }
  if (!state.connected || !state.micActive || !state.vadStreaming) {
    return;
  }
  if (state.presence === "speaking" && !state.bargeInActive) {
    return;
  }
  if (state.vadSilenceFrames < 1) {
    return;
  }
  maybeAutoCommit();
}

function handleAssistantThinkingDelta(msg) {
  if (state.bargeInActive || !state.connected) {
    return;
  }
  const text = String(msg.text_delta || "").trim();
  if (!text) {
    return;
  }
  if (state.awaitingAssistantResponse) {
    setPresence("thinking", "", "");
  }
  setCaption("", softTruncate(text, 160), { clearAfterMs: 1700 });
}

function handleSystemEvent(msg) {
  const code = String(msg.code || "").toLowerCase();
  switch (code) {
    case "wake_word":
      state.awakeUntilMs = Date.now() + HANDSFREE_AWAKE_WINDOW_MS;
      setPresence("listening", "Yes?", "");
      setCaption("Yes?", "", { clearAfterMs: 1200 });
      logEvent("wake word detected");
      break;
    case "assistant_working":
      if (!state.awaitingAssistantResponse) {
        break;
      }
      setPresence("thinking", "", "");
      scheduleSilenceBreaker();
      scheduleThinkingFiller();
      logEvent("assistant working");
      break;
    case "assistant_first_text":
      state.awaitingAssistantResponse = false;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      cancelSpokenFiller();
      setPresence("thinking", "", "");
      logEvent("assistant first text");
      break;
    case "assistant_first_audio":
      state.awaitingAssistantResponse = false;
      clearSilenceBreakerTimer();
      clearFillerTimer();
      cancelSpokenFiller();
      state.speakEnergyTarget = Math.max(state.speakEnergyTarget, 0.32);
      setPresence("speaking", "…", "");
      logEvent("assistant first audio");
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
  state.awaitingAssistantResponse = false;
  clearSilenceBreakerTimer();
  clearFillerTimer();
  cancelSpokenFiller();
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

  if (state.handsFree) {
    // After Samantha finishes talking, keep the awake window open briefly so the user can reply naturally.
    state.awakeUntilMs = Date.now() + HANDSFREE_AWAKE_WINDOW_MS;
    state.manualArmUntilMs = 0;
  } else {
    state.awakeUntilMs = 0;
    state.manualArmUntilMs = 0;
  }

  if (state.micActive && state.handsFree) {
    setPresence("connected", "Samantha", "");
  } else if (state.micActive) {
    setPresence("listening", "I’m listening.", "");
  } else if (state.connected) {
    setPresence("connected", "Samantha", "Ready.");
  }
  logEvent(`assistant turn ended: ${reason}`);
}

function resetTaskDeskState(sessionID) {
  const td = state.taskDesk;
  clearTaskDeskFallbackTimer();
  td.sessionId = String(sessionID || "").trim();
  td.syncState = td.runtimeEnabled === false ? "disabled" : "idle";
  td.lastSyncAtMs = 0;
  td.tasksById.clear();
  td.timelinesByTask.clear();
  td.orderedActive = [];
  td.orderedAwaiting = [];
  td.orderedPlanned = [];
  td.eventSignatures.clear();
  td.eventOrder = [];
  td.actionInFlight.clear();
  td.bootstrapPromise = null;
  td.bootstrapForSession = "";
  td.snapshotSeen = false;
  renderTaskDesk();
}

function clearTaskDeskFallbackTimer() {
  const td = state.taskDesk;
  if (!td.fallbackTimer) {
    return;
  }
  clearTimeout(td.fallbackTimer);
  td.fallbackTimer = null;
}

function markTaskDeskSyncState(nextState) {
  const td = state.taskDesk;
  td.syncState = String(nextState || "idle");
  if (td.syncState === "synced" || td.syncState === "snapshot_received") {
    td.lastSyncAtMs = Date.now();
  }
}

function markTaskDeskConnected() {
  const td = state.taskDesk;
  if (!isTaskDeskVisible()) {
    clearTaskDeskFallbackTimer();
    return;
  }
  const sessionID = (state.sessionId || "").trim();
  if (!sessionID) {
    return;
  }
  if (td.sessionId !== sessionID) {
    resetTaskDeskState(sessionID);
  }
  td.snapshotSeen = false;
  if (td.runtimeEnabled === false) {
    markTaskDeskSyncState("disabled");
    renderTaskDesk();
    return;
  }
  clearTaskDeskFallbackTimer();
  td.fallbackTimer = setTimeout(() => {
    if (!state.connected) {
      return;
    }
    if (td.snapshotSeen) {
      return;
    }
    void bootstrapTaskDesk(sessionID, "ws_open_fallback");
  }, TASK_RESYNC_FALLBACK_MS);
  renderTaskDesk();
}

function markTaskDeskDisconnected() {
  const td = state.taskDesk;
  clearTaskDeskFallbackTimer();
  if (!isTaskDeskVisible()) {
    return;
  }
  if (td.runtimeEnabled === false) {
    markTaskDeskSyncState("disabled");
    return;
  }
  if (td.syncState !== "idle") {
    markTaskDeskSyncState("degraded");
  }
}

function normalizeTaskStatus(status) {
  const s = String(status || "").trim().toLowerCase();
  if (!s) {
    return "planned";
  }
  if (
    s === "cancelled" ||
    s === "failed" ||
    s === "completed" ||
    s === "running" ||
    s === "paused" ||
    s === "awaiting_approval" ||
    s === "planned"
  ) {
    return s;
  }
  return "planned";
}

function isTaskTerminalStatus(status) {
  const s = normalizeTaskStatus(status);
  return s === "completed" || s === "failed" || s === "cancelled";
}

function parseTimestampMs(value) {
  if (typeof value === "number" && Number.isFinite(value) && value > 0) {
    return value;
  }
  if (typeof value === "string" && value.trim()) {
    const ms = Date.parse(value);
    if (Number.isFinite(ms) && ms > 0) {
      return ms;
    }
  }
  return Date.now();
}

function taskRiskValue(raw) {
  const s = String(raw || "").trim().toLowerCase();
  if (s === "high" || s === "medium" || s === "low") {
    return s;
  }
  return "";
}

function taskEventSignature(evt) {
  return [
    String(evt?.task_id || ""),
    String(evt?.type || ""),
    String(evt?.step_id || ""),
    String(evt?.status || ""),
    String(evt?.code || ""),
    String(evt?.detail || ""),
    String(evt?.text_delta || ""),
    String(evt?.result || ""),
    String(evt?.at || ""),
  ].join("|");
}

function rememberTaskEventSignature(signature) {
  const td = state.taskDesk;
  if (!signature) {
    return false;
  }
  if (td.eventSignatures.has(signature)) {
    td.metrics.task_event_dedup_hit += 1;
    return true;
  }
  td.eventSignatures.add(signature);
  td.eventOrder.push(signature);
  while (td.eventOrder.length > TASK_EVENT_DEDUP_MAX) {
    const old = td.eventOrder.shift();
    if (old) {
      td.eventSignatures.delete(old);
    }
  }
  return false;
}

function appendTaskTimeline(taskID, entry) {
  const td = state.taskDesk;
  const items = td.timelinesByTask.get(taskID) || [];
  items.push(entry);
  while (items.length > 12) {
    items.shift();
  }
  td.timelinesByTask.set(taskID, items);
}

function latestTaskTimelineNote(taskID) {
  const td = state.taskDesk;
  const items = td.timelinesByTask.get(taskID) || [];
  if (items.length === 0) {
    return "";
  }
  const last = items[items.length - 1];
  return String(last?.text || "").trim();
}

function normalizeTaskFromSnapshotItem(item, status) {
  const taskID = String(item?.task_id || "").trim();
  if (!taskID) {
    return null;
  }
  return {
    id: taskID,
    summary: String(item?.summary || "").trim() || `Task ${taskID.slice(0, 8)}`,
    status: normalizeTaskStatus(status || item?.status),
    riskLevel: taskRiskValue(item?.risk_level),
    requiresApproval: Boolean(item?.requires_approval),
    updatedAtMs: Date.now(),
  };
}

function currentStepFromTask(task) {
  if (!task || !Array.isArray(task.steps) || task.steps.length === 0) {
    return null;
  }
  const currentID = String(task.current_step_id || "").trim();
  if (currentID) {
    for (const step of task.steps) {
      if (String(step?.id || "").trim() === currentID) {
        return step;
      }
    }
  }
  return task.steps[task.steps.length - 1];
}

function normalizeTaskFromAPI(task) {
  const taskID = String(task?.id || "").trim();
  if (!taskID) {
    return null;
  }
  const step = currentStepFromTask(task);
  const note = step?.output_redacted || step?.error || task?.error || task?.result || "";
  return {
    id: taskID,
    summary: String(task?.summary || task?.intent_text || "").trim() || `Task ${taskID.slice(0, 8)}`,
    status: normalizeTaskStatus(task?.status),
    riskLevel: taskRiskValue(task?.risk_level),
    requiresApproval: Boolean(task?.requires_approval),
    updatedAtMs: parseTimestampMs(task?.updated_at || task?.created_at),
    stepTitle: String(step?.title || "").trim(),
    note: String(note || "").trim(),
    error: String(task?.error || "").trim(),
  };
}

function eventNoteFromMessage(msg) {
  const type = String(msg?.type || "").trim();
  switch (type) {
    case "task_created":
      return "Task created.";
    case "task_plan_graph": {
      const nodes = Array.isArray(msg?.nodes) ? msg.nodes.length : 0;
      if (nodes > 0) {
        return `Planned ${nodes} step${nodes === 1 ? "" : "s"}.`;
      }
      return String(msg?.detail || "").trim() || "Task graph planned.";
    }
    case "task_plan_delta":
      return String(msg?.text_delta || "").trim() || "Task plan updated.";
    case "task_step_started":
      return String(msg?.title || "").trim() ? `Started: ${String(msg.title).trim()}` : "Task step started.";
    case "task_step_log":
      return String(msg?.text_delta || "").trim() || "";
    case "task_step_completed":
      return "Task step completed.";
    case "task_waiting_approval":
      return String(msg?.prompt || "").trim() || "Waiting for approval.";
    case "task_completed":
      return String(msg?.result || "").trim() || "Task completed.";
    case "task_failed":
      return String(msg?.detail || msg?.code || "").trim() || "Task failed.";
    default:
      return "";
  }
}

function upsertTaskDeskTask(patch) {
  if (!patch || !patch.id) {
    return;
  }
  const td = state.taskDesk;
  const prev = td.tasksById.get(patch.id) || {};
  const merged = {
    id: patch.id,
    summary: String(patch.summary || prev.summary || `Task ${patch.id.slice(0, 8)}`).trim(),
    status: normalizeTaskStatus(patch.status || prev.status || "planned"),
    riskLevel: taskRiskValue(patch.riskLevel || prev.riskLevel),
    requiresApproval: typeof patch.requiresApproval === "boolean" ? patch.requiresApproval : Boolean(prev.requiresApproval),
    updatedAtMs: Math.max(Number(prev.updatedAtMs) || 0, Number(patch.updatedAtMs) || Date.now()),
    stepTitle: String(patch.stepTitle || prev.stepTitle || "").trim(),
    note: String(patch.note || prev.note || "").trim(),
    error: String(patch.error || prev.error || "").trim(),
  };

  if (!merged.requiresApproval && merged.status !== "awaiting_approval") {
    td.actionInFlight.delete(merged.id);
  }
  if (isTaskTerminalStatus(merged.status)) {
    td.actionInFlight.delete(merged.id);
  }
  td.tasksById.set(merged.id, merged);
}

function recomputeTaskDeskOrder() {
  const td = state.taskDesk;
  const all = Array.from(td.tasksById.values());
  const byUpdatedDesc = (a, b) => (b.updatedAtMs || 0) - (a.updatedAtMs || 0);
  td.orderedActive = all.filter((t) => t.status === "running").sort(byUpdatedDesc).map((t) => t.id);
  td.orderedAwaiting = all.filter((t) => t.status === "awaiting_approval").sort(byUpdatedDesc).map((t) => t.id);
  td.orderedPlanned = all.filter((t) => t.status === "planned" || t.status === "paused").sort(byUpdatedDesc).map((t) => t.id);
}

function taskSyncLabel() {
  const td = state.taskDesk;
  const stateName = String(td.syncState || "idle");
  switch (stateName) {
    case "snapshot_received":
      return "snapshot";
    case "hydrating":
      return "syncing";
    case "synced":
      return td.lastSyncAtMs > 0 ? `${relativeTime(td.lastSyncAtMs)} sync` : "synced";
    case "degraded":
      return "degraded";
    case "disabled":
      return "disabled";
    default:
      return "idle";
  }
}

function renderTaskDeskGroup(root, ids, emptyText) {
  if (!root) {
    return;
  }
  root.innerHTML = "";
  const td = state.taskDesk;
  if (!ids || ids.length === 0) {
    const row = document.createElement("p");
    row.className = "task-empty";
    row.textContent = emptyText;
    root.appendChild(row);
    return;
  }

  for (const id of ids) {
    const task = td.tasksById.get(id);
    if (!task) {
      continue;
    }

    const card = document.createElement("article");
    card.className = "task-card";
    if (task.riskLevel) {
      card.setAttribute("data-risk", task.riskLevel);
    }

    const top = document.createElement("div");
    top.className = "task-top";

    const summary = document.createElement("div");
    summary.className = "task-summary";
    summary.textContent = softTruncate(task.summary || "Task", 120);

    const meta = document.createElement("div");
    meta.className = "task-meta";
    const status = document.createElement("span");
    status.className = "task-status";
    status.textContent = String(task.status || "planned").replaceAll("_", " ");
    meta.appendChild(status);
    if (task.riskLevel) {
      const risk = document.createElement("span");
      risk.textContent = task.riskLevel;
      meta.appendChild(risk);
    }
    const updated = document.createElement("span");
    updated.textContent = relativeTime(task.updatedAtMs);
    meta.appendChild(updated);

    top.appendChild(summary);
    top.appendChild(meta);
    card.appendChild(top);

    const noteText = latestTaskTimelineNote(task.id) || task.note || task.error || "";
    if (noteText) {
      const note = document.createElement("div");
      note.className = "task-note";
      note.textContent = softTruncate(noteText, 180);
      card.appendChild(note);
    }

    const actions = document.createElement("div");
    actions.className = "task-actions";
    const inFlight = td.actionInFlight.has(task.id);

    if (task.status === "awaiting_approval") {
      const approve = document.createElement("button");
      approve.type = "button";
      approve.className = "task-action";
      approve.textContent = "Approve";
      approve.setAttribute("data-task-id", task.id);
      approve.setAttribute("data-task-action", "approve");
      approve.disabled = inFlight;
      actions.appendChild(approve);

      const deny = document.createElement("button");
      deny.type = "button";
      deny.className = "task-action";
      deny.textContent = "Deny";
      deny.setAttribute("data-task-id", task.id);
      deny.setAttribute("data-task-action", "deny");
      deny.disabled = inFlight;
      actions.appendChild(deny);
    }

    if (task.status === "running") {
      const pause = document.createElement("button");
      pause.type = "button";
      pause.className = "task-action";
      pause.textContent = "Pause";
      pause.setAttribute("data-task-id", task.id);
      pause.setAttribute("data-task-action", "pause");
      pause.disabled = inFlight;
      actions.appendChild(pause);
    }

    if (task.status === "paused") {
      const resume = document.createElement("button");
      resume.type = "button";
      resume.className = "task-action";
      resume.textContent = "Resume";
      resume.setAttribute("data-task-id", task.id);
      resume.setAttribute("data-task-action", "resume");
      resume.disabled = inFlight;
      actions.appendChild(resume);
    }

    if (!isTaskTerminalStatus(task.status)) {
      const cancel = document.createElement("button");
      cancel.type = "button";
      cancel.className = "task-action is-danger";
      cancel.textContent = "Cancel";
      cancel.setAttribute("data-task-id", task.id);
      cancel.setAttribute("data-task-action", "cancel");
      cancel.disabled = inFlight;
      actions.appendChild(cancel);
    }

    if (actions.children.length > 0) {
      card.appendChild(actions);
    }
    root.appendChild(card);
  }
}

function renderTaskDesk() {
  if (!isTaskDeskVisible()) {
    return;
  }
  const td = state.taskDesk;
  recomputeTaskDeskOrder();

  if (el.taskCountActive) {
    el.taskCountActive.textContent = String(td.orderedActive.length);
  }
  if (el.taskCountAwaiting) {
    el.taskCountAwaiting.textContent = String(td.orderedAwaiting.length);
  }
  if (el.taskCountPlanned) {
    el.taskCountPlanned.textContent = String(td.orderedPlanned.length);
  }

  if (el.taskSync) {
    el.taskSync.textContent = taskSyncLabel();
    el.taskSync.classList.remove("is-synced", "is-hydrating", "is-degraded");
    if (td.syncState === "synced" || td.syncState === "snapshot_received") {
      el.taskSync.classList.add("is-synced");
    } else if (td.syncState === "hydrating") {
      el.taskSync.classList.add("is-hydrating");
    } else if (td.syncState === "degraded") {
      el.taskSync.classList.add("is-degraded");
    }
  }

  if (td.runtimeEnabled === false) {
    renderTaskDeskGroup(el.taskListActive, [], "Task runtime disabled.");
    renderTaskDeskGroup(el.taskListAwaiting, [], "Enable APP_TASK_RUNTIME_ENABLED to use tasks.");
    renderTaskDeskGroup(el.taskListPlanned, [], "No queued tasks.");
    return;
  }

  renderTaskDeskGroup(el.taskListActive, td.orderedActive, "No active tasks.");
  renderTaskDeskGroup(el.taskListAwaiting, td.orderedAwaiting, "No approvals needed.");
  renderTaskDeskGroup(el.taskListPlanned, td.orderedPlanned, "No queued tasks.");
}

async function bootstrapTaskDesk(sessionID, reason) {
  const td = state.taskDesk;
  const targetSession = String(sessionID || "").trim();
  if (!isTaskDeskVisible() || !targetSession || td.runtimeEnabled === false) {
    return;
  }
  if (td.bootstrapPromise && td.bootstrapForSession === targetSession) {
    return td.bootstrapPromise;
  }

  markTaskDeskSyncState("hydrating");
  renderTaskDesk();

  td.bootstrapForSession = targetSession;
  td.bootstrapPromise = (async () => {
    const listURL = `/v1/tasks?session_id=${encodeURIComponent(targetSession)}&limit=${TASK_BOOTSTRAP_LIMIT}`;
    const res = await fetch(listURL, { cache: "no-store" });
    if (res.status === 501) {
      td.runtimeEnabled = false;
      markTaskDeskSyncState("disabled");
      renderTaskDesk();
      return;
    }
    if (!res.ok) {
      throw new Error(`tasks list failed: HTTP ${res.status}`);
    }
    const payload = await res.json();
    if (td.sessionId !== targetSession) {
      return;
    }
    td.runtimeEnabled = true;

    const tasks = Array.isArray(payload?.tasks) ? payload.tasks : [];
    const seenIDs = new Set();
    for (const rawTask of tasks) {
      const normalized = normalizeTaskFromAPI(rawTask);
      if (!normalized) {
        continue;
      }
      seenIDs.add(normalized.id);
      upsertTaskDeskTask(normalized);
    }
    for (const [taskID, task] of td.tasksById.entries()) {
      if (!seenIDs.has(taskID) && !isTaskTerminalStatus(task.status)) {
        td.tasksById.delete(taskID);
      }
    }

    const nonTerminalIDs = [];
    for (const rawTask of tasks) {
      const taskID = String(rawTask?.id || "").trim();
      const status = normalizeTaskStatus(rawTask?.status);
      if (!taskID || isTaskTerminalStatus(status)) {
        continue;
      }
      nonTerminalIDs.push(taskID);
    }
    await Promise.all(nonTerminalIDs.map((taskID) => hydrateTaskEvents(taskID, targetSession)));

    td.metrics.task_bootstrap_success += 1;
    markTaskDeskSyncState("synced");
    td.lastSyncAtMs = Date.now();
    renderTaskDesk();
    logEvent(`task bootstrap ok (${reason || "unknown"})`);
  })().catch((err) => {
    if (td.sessionId === targetSession) {
      td.metrics.task_bootstrap_error += 1;
      markTaskDeskSyncState("degraded");
      renderTaskDesk();
      logError(`task bootstrap failed: ${stringifyError(err)}`);
    }
  }).finally(() => {
    if (td.bootstrapForSession === targetSession) {
      td.bootstrapPromise = null;
      td.bootstrapForSession = "";
    }
  });
  return td.bootstrapPromise;
}

async function hydrateTaskEvents(taskID, expectedSessionID) {
  const id = String(taskID || "").trim();
  const expected = String(expectedSessionID || state.taskDesk.sessionId || "").trim();
  if (!isTaskDeskVisible() || !id || state.taskDesk.runtimeEnabled === false) {
    return;
  }
  const res = await fetch(`/v1/tasks/${encodeURIComponent(id)}/events?limit=${TASK_EVENTS_LIMIT}`, { cache: "no-store" });
  if (!res.ok) {
    return;
  }
  if (expected && state.taskDesk.sessionId !== expected) {
    return;
  }
  const payload = await res.json();
  const events = Array.isArray(payload?.events) ? payload.events : [];
  for (const evt of events) {
    if (expected && state.taskDesk.sessionId !== expected) {
      return;
    }
    const merged = {
      ...evt,
      task_id: String(evt?.task_id || id).trim(),
    };
    applyTaskEventPatch(merged, { source: "bootstrap", suppressUI: true });
  }
  renderTaskDesk();
}

async function refreshTask(taskID, opts = {}) {
  const id = String(taskID || "").trim();
  if (!isTaskDeskVisible() || !id || state.taskDesk.runtimeEnabled === false) {
    return;
  }
  const delayMS = Number(opts.delayMS) || 0;
  if (delayMS > 0) {
    await new Promise((resolve) => setTimeout(resolve, delayMS));
  }
  const res = await fetch(`/v1/tasks/${encodeURIComponent(id)}`, { cache: "no-store" });
  if (!res.ok) {
    return;
  }
  const payload = await res.json();
  const normalized = normalizeTaskFromAPI(payload);
  if (normalized) {
    upsertTaskDeskTask(normalized);
    renderTaskDesk();
  }
  await hydrateTaskEvents(id, state.taskDesk.sessionId);
}

async function handleTaskDeskAction(taskID, action) {
  const td = state.taskDesk;
  const id = String(taskID || "").trim();
  const op = String(action || "").trim();
  if (!isTaskDeskVisible() || !id || !op) {
    return;
  }
  if (td.actionInFlight.has(id)) {
    return;
  }

  td.actionInFlight.set(id, op);
  renderTaskDesk();

  try {
    if (op === "approve") {
      const ok = sendControl("approve_task_step", { taskID: id, approved: true });
      if (!ok) {
        throw new Error("websocket not connected");
      }
      setCaption("Approval sent.", "", { clearAfterMs: 1400 });
      await refreshTask(id, { delayMS: TASK_REFRESH_AFTER_CONTROL_MS });
      return;
    }
    if (op === "deny") {
      const ok = sendControl("deny_task_step", { taskID: id, approved: false });
      if (!ok) {
        throw new Error("websocket not connected");
      }
      setCaption("Denial sent.", "", { clearAfterMs: 1400 });
      await refreshTask(id, { delayMS: TASK_REFRESH_AFTER_CONTROL_MS });
      return;
    }
    if (op === "cancel") {
      const res = await fetch(`/v1/tasks/${encodeURIComponent(id)}/cancel`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ reason: "Cancelled from Task Desk." }),
      });
      if (!res.ok) {
        throw new Error(`cancel failed: HTTP ${res.status}`);
      }
      const payload = await res.json();
      const normalized = normalizeTaskFromAPI(payload);
      if (normalized) {
        upsertTaskDeskTask(normalized);
      }
      await hydrateTaskEvents(id, state.taskDesk.sessionId);
      setCaption("Cancel sent.", "", { clearAfterMs: 1400 });
      return;
    }
    if (op === "pause") {
      const ok = sendControl("pause_task", { taskID: id });
      if (!ok) {
        const res = await fetch(`/v1/tasks/${encodeURIComponent(id)}/pause`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ reason: "Paused from Task Desk." }),
        });
        if (!res.ok) {
          throw new Error(`pause failed: HTTP ${res.status}`);
        }
      }
      setCaption("Pause sent.", "", { clearAfterMs: 1400 });
      await refreshTask(id, { delayMS: TASK_REFRESH_AFTER_CONTROL_MS });
      return;
    }
    if (op === "resume") {
      const ok = sendControl("resume_task", { taskID: id });
      if (!ok) {
        const res = await fetch(`/v1/tasks/${encodeURIComponent(id)}/resume`, {
          method: "POST",
        });
        if (!res.ok) {
          throw new Error(`resume failed: HTTP ${res.status}`);
        }
      }
      setCaption("Resume sent.", "", { clearAfterMs: 1400 });
      await refreshTask(id, { delayMS: TASK_REFRESH_AFTER_CONTROL_MS });
      return;
    }
  } catch (err) {
    const task = td.tasksById.get(id);
    if (task) {
      task.error = stringifyError(err);
      td.tasksById.set(id, task);
    }
    markTaskDeskSyncState("degraded");
    logError(`task action failed: ${op} ${id} ${stringifyError(err)}`);
  } finally {
    td.actionInFlight.delete(id);
    renderTaskDesk();
  }
}

function applyTaskSnapshot(msg) {
  const active = Array.isArray(msg?.active) ? msg.active : [];
  const awaiting = Array.isArray(msg?.awaiting_approval) ? msg.awaiting_approval : [];
  const planned = Array.isArray(msg?.planned) ? msg.planned : [];
  const td = state.taskDesk;

  for (const [taskID, task] of td.tasksById.entries()) {
    if (!isTaskTerminalStatus(task.status)) {
      td.tasksById.delete(taskID);
    }
  }

  for (const item of active) {
    const normalized = normalizeTaskFromSnapshotItem(item, "running");
    if (normalized) {
      upsertTaskDeskTask(normalized);
    }
  }
  for (const item of awaiting) {
    const normalized = normalizeTaskFromSnapshotItem(item, "awaiting_approval");
    if (normalized) {
      upsertTaskDeskTask(normalized);
    }
  }
  for (const item of planned) {
    const normalized = normalizeTaskFromSnapshotItem(item, "planned");
    if (normalized) {
      upsertTaskDeskTask(normalized);
    }
  }
}

function applyTaskEventPatch(msg, opts = {}) {
  const taskID = String(msg?.task_id || "").trim();
  if (!taskID) {
    return false;
  }
  const signature = taskEventSignature(msg);
  if (rememberTaskEventSignature(signature)) {
    return false;
  }

  const type = String(msg?.type || "").trim();
  const updatedAtMs = parseTimestampMs(msg?.at);
  const patch = {
    id: taskID,
    updatedAtMs,
    riskLevel: taskRiskValue(msg?.risk_level),
  };
  let statusSet = false;
  switch (type) {
    case "task_created":
      patch.summary = String(msg?.summary || "").trim();
      patch.status = normalizeTaskStatus(msg?.status || "planned");
      patch.requiresApproval = Boolean(msg?.requires_approval);
      statusSet = true;
      break;
    case "task_plan_graph":
      patch.status = normalizeTaskStatus(msg?.status || "planned");
      patch.note = String(msg?.detail || "").trim();
      statusSet = true;
      break;
    case "task_plan_delta":
      patch.status = normalizeTaskStatus(msg?.status || "planned");
      patch.note = String(msg?.text_delta || "").trim();
      statusSet = true;
      break;
    case "task_step_started":
      patch.status = "running";
      patch.stepTitle = String(msg?.title || "").trim();
      statusSet = true;
      break;
    case "task_step_log":
      patch.note = String(msg?.text_delta || "").trim();
      break;
    case "task_step_completed":
      if (msg?.status) {
        patch.status = normalizeTaskStatus(msg?.status);
        statusSet = true;
      }
      break;
    case "task_waiting_approval":
      patch.status = "awaiting_approval";
      patch.requiresApproval = true;
      patch.note = String(msg?.prompt || "").trim();
      statusSet = true;
      break;
    case "task_completed":
      patch.status = "completed";
      patch.requiresApproval = false;
      patch.note = String(msg?.result || "").trim();
      statusSet = true;
      break;
    case "task_failed":
      patch.status = normalizeTaskStatus(msg?.status || (String(msg?.code || "").trim() === "cancelled" ? "cancelled" : "failed"));
      patch.requiresApproval = false;
      patch.error = String(msg?.detail || msg?.code || "").trim();
      statusSet = true;
      break;
    default:
      break;
  }
  if (!statusSet) {
    const existing = state.taskDesk.tasksById.get(taskID);
    if (existing) {
      patch.status = existing.status;
    }
  }
  upsertTaskDeskTask(patch);

  const note = eventNoteFromMessage(msg);
  if (note) {
    appendTaskTimeline(taskID, {
      type,
      atMs: updatedAtMs,
      text: note,
    });
  }

  if (!opts.suppressUI) {
    switch (type) {
      case "task_created":
        setPresence("thinking", "Planning task…", "");
        setCaption("Task created.", note || `Task ${taskID.slice(0, 8)}`, { clearAfterMs: 2200 });
        logEvent(`task created: ${taskID}`);
        break;
      case "task_plan_graph":
        setPresence("thinking", "Planning task…", "");
        if (note) {
          setCaption("Task graph planned.", softTruncate(note, 180), { clearAfterMs: 2200 });
        }
        logEvent(`task graph: ${taskID}`);
        break;
      case "task_plan_delta":
        setPresence("thinking", "Planning task…", "");
        if (note) {
          setCaption("Task planning…", softTruncate(note, 180), { clearAfterMs: 2200 });
        }
        logEvent(`task plan: ${taskID}`);
        break;
      case "task_step_started":
        setPresence("thinking", "Running task…", "");
        setCaption("Working on it.", softTruncate(note, 170), { clearAfterMs: 2200 });
        logEvent(`task step started: ${taskID}`);
        break;
      case "task_step_log":
        if (note) {
          setCaption("Working on it.", softTruncate(note, 200), { clearAfterMs: 2800 });
        }
        logEvent(`task step log: ${taskID}`);
        break;
      case "task_step_completed":
        setCaption("Step completed.", "", { clearAfterMs: 1600 });
        logEvent(`task step completed: ${taskID}`);
        break;
      case "task_waiting_approval":
        setPresence("connected", "Approval needed.", "");
        setCaption("Approval required.", softTruncate(note || "Say “approve task” or “deny task”.", 220), { clearAfterMs: 5200 });
        logEvent(`task waiting approval: ${taskID}`);
        break;
      case "task_failed":
        if (normalizeTaskStatus(msg?.status) === "paused") {
          setPresence("connected", "Task paused.", "");
          setCaption("Task paused.", softTruncate(note || "Say “resume task” when ready.", 220), { clearAfterMs: 3200 });
          logEvent(`task paused: ${taskID}`);
          break;
        }
        setPresence("error", "Task failed.", softTruncate(note, 220));
        logError(`task failed: ${taskID} ${note}`.trim());
        break;
      case "task_completed":
        setPresence("connected", "Task completed.", "");
        setCaption("Task completed.", softTruncate(note, 220), { clearAfterMs: 3800 });
        logEvent(`task completed: ${taskID}`);
        break;
      default:
        logEvent(`task event: ${type}`);
        break;
    }
  }
  return true;
}

function handleTaskSnapshot(msg) {
  if (!isTaskDeskVisible()) {
    return;
  }
  const td = state.taskDesk;
  td.runtimeEnabled = true;
  td.metrics.task_snapshot_received += 1;
  td.snapshotSeen = true;
  clearTaskDeskFallbackTimer();

  applyTaskSnapshot(msg);
  markTaskDeskSyncState("snapshot_received");
  renderTaskDesk();

  const awaiting = Array.isArray(msg?.awaiting_approval) ? msg.awaiting_approval : [];
  if (awaiting.length > 0) {
    setPresence("connected", "Approval needed.", "");
    setCaption("Approval required.", `${awaiting.length} task(s) waiting for approval.`, { clearAfterMs: 3200 });
  }
  logEvent(`task snapshot: active=${Array.isArray(msg?.active) ? msg.active.length : 0} awaiting=${awaiting.length} planned=${Array.isArray(msg?.planned) ? msg.planned.length : 0}`);
  if (state.sessionId) {
    void bootstrapTaskDesk(state.sessionId, "snapshot");
  }
}

function handleTaskEvent(msg) {
  if (!isTaskDeskVisible()) {
    return;
  }
  applyTaskEventPatch(msg, { source: "live", suppressUI: false });
  renderTaskDesk();
}

function relativeTime(whenMs) {
  const ms = Number(whenMs) || 0;
  if (ms <= 0) {
    return "just now";
  }
  const delta = Math.max(0, Date.now() - ms);
  const sec = Math.floor(delta / 1000);
  if (sec < 3) {
    return "just now";
  }
  if (sec < 60) {
    return `${sec}s ago`;
  }
  const min = Math.floor(sec / 60);
  if (min < 60) {
    return `${min}m ago`;
  }
  const hr = Math.floor(min / 60);
  if (hr < 24) {
    return `${hr}h ago`;
  }
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
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

  if (normalized !== "thinking") {
    clearSilenceBreakerTimer();
    clearThinkingCueTimer();
    clearFillerTimer();
    cancelSpokenFiller();
  }

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

function clearSilenceBreakerTimer() {
  if (!state.silenceBreakerTimer) {
    return;
  }
  clearTimeout(state.silenceBreakerTimer);
  state.silenceBreakerTimer = null;
}

function clearThinkingCueTimer() {
  if (!state.thinkingCueTimer) {
    return;
  }
  clearTimeout(state.thinkingCueTimer);
  state.thinkingCueTimer = null;
}

function clearFillerTimer() {
  if (!state.fillerTimer) {
    return;
  }
  clearTimeout(state.fillerTimer);
  state.fillerTimer = null;
}

function cancelSpokenFiller() {
  if (!window.speechSynthesis || typeof window.speechSynthesis.cancel !== "function") {
    return;
  }
  try {
    window.speechSynthesis.cancel();
  } catch (_err) {
    // ignore
  }
}

function pickFillerPhrase() {
  if (!Array.isArray(FILLER_PHRASES) || FILLER_PHRASES.length === 0) {
    return "";
  }
  const available = FILLER_PHRASES.filter((s) => s && s !== state.lastFillerPhrase);
  const pool = available.length > 0 ? available : FILLER_PHRASES.slice();
  const idx = Math.floor(Math.random() * pool.length);
  const phrase = String(pool[idx] || "").trim();
  if (!phrase) {
    return "";
  }
  state.lastFillerPhrase = phrase;
  return phrase;
}

function speakThinkingFiller() {
  const mode = normalizeFillerMode(state.uiSettings.fillerMode);
  if (mode === "off") {
    return;
  }
  if (!state.connected || !state.awaitingAssistantResponse || state.presence !== "thinking") {
    return;
  }
  const maxPerTurn = normalizeFillerMaxPerTurn(state.uiSettings.fillerMaxPerTurn);
  if (state.fillerCountForTurn >= maxPerTurn) {
    return;
  }
  const now = Date.now();
  const cooldownMs = normalizeFillerCooldownMs(state.uiSettings.fillerCooldownMs);
  if (now - state.lastFillerAtMs < cooldownMs) {
    return;
  }
  const minDelayMs = normalizeFillerDelayMs(state.uiSettings.fillerMinDelayMs);
  const elapsedSinceCommit = state.lastCommitAtMs > 0 ? now - state.lastCommitAtMs : 0;
  if (mode === "adaptive") {
    const adaptiveFloor = minDelayMs + 900;
    if (elapsedSinceCommit < adaptiveFloor) {
      return;
    }
  } else if (mode === "occasional") {
    if (elapsedSinceCommit < minDelayMs) {
      return;
    }
  }

  let text = "";
  if (mode === "adaptive") {
    text = "Thinking...";
  } else {
    text = pickFillerPhrase();
  }
  if (!text) {
    return;
  }

  state.lastFillerAtMs = now;
  state.fillerCountForTurn += 1;

  const spokenAllowed = mode === "always";
  if (spokenAllowed && window.speechSynthesis && typeof window.SpeechSynthesisUtterance === "function") {
    try {
      const utt = new window.SpeechSynthesisUtterance(text);
      utt.rate = 1.0;
      utt.pitch = 1.0;
      utt.volume = 0.6;
      window.speechSynthesis.speak(utt);
    } catch (_err) {
      // ignore; fallback to visual cue only
    }
  }
  setCaption("", text, { clearAfterMs: mode === "adaptive" ? 900 : 1400 });
}

function scheduleThinkingFiller() {
  clearFillerTimer();
  const mode = normalizeFillerMode(state.uiSettings.fillerMode);
  if (mode === "off") {
    return;
  }
  if (!state.connected || !state.awaitingAssistantResponse || state.presence !== "thinking") {
    return;
  }
  let delayMs = 0;
  const minDelayMs = normalizeFillerDelayMs(state.uiSettings.fillerMinDelayMs);
  const elapsed = state.lastCommitAtMs > 0 ? Date.now() - state.lastCommitAtMs : 0;
  if (mode === "adaptive") {
    const adaptiveFloor = minDelayMs + 900;
    delayMs = Math.max(0, adaptiveFloor - elapsed);
  } else if (mode === "occasional") {
    delayMs = Math.max(0, minDelayMs - elapsed);
  }
  state.fillerTimer = setTimeout(() => {
    state.fillerTimer = null;
    speakThinkingFiller();
  }, delayMs);
}

function scheduleSilenceBreaker() {
  clearSilenceBreakerTimer();
  clearThinkingCueTimer();
  if (!state.connected || !state.awaitingAssistantResponse) {
    return;
  }
  const mode = normalizeSilenceBreakerMode(state.uiSettings.silenceBreakerMode);
  if (mode === "off") {
    return;
  }
  const delayMs = normalizeSilenceBreakerDelayMs(state.uiSettings.silenceBreakerDelayMs);
  state.silenceBreakerTimer = setTimeout(() => {
    state.silenceBreakerTimer = null;
    if (!state.connected || state.presence !== "thinking" || !state.awaitingAssistantResponse) {
      return;
    }
    if (mode === "visual") {
      setCaption("", "...", { clearAfterMs: 1200 });
      return;
    }
    if (mode === "speech") {
      setCaption("", "...", { clearAfterMs: 1200 });
      void playThinkingCue();
    }
  }, delayMs);
}

async function playThinkingCue() {
  if (state.presence !== "thinking" || !state.connected) {
    return;
  }
  const now = Date.now();
  if (now - state.lastThinkingCueAtMs < THINKING_CUE_MIN_INTERVAL_MS) {
    return;
  }

  let ctx;
  try {
    ctx = await ensurePlaybackContext();
  } catch (_err) {
    return;
  }
  if (!ctx || state.presence !== "thinking") {
    return;
  }
  if (state.playbackContext !== ctx || !state.playbackGain) {
    return;
  }

  const t0 = Math.max(ctx.currentTime, state.playbackNextTime || 0);
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();

  osc.type = "sine";
  osc.frequency.setValueAtTime(THINKING_CUE_FREQ_START_HZ, t0);
  osc.frequency.exponentialRampToValueAtTime(THINKING_CUE_FREQ_END_HZ, t0 + THINKING_CUE_DURATION_SEC);

  gain.gain.setValueAtTime(0.0001, t0);
  gain.gain.exponentialRampToValueAtTime(THINKING_CUE_GAIN, t0 + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.0001, t0 + THINKING_CUE_DURATION_SEC);

  osc.connect(gain);
  gain.connect(state.playbackGain);
  osc.start(t0);
  osc.stop(t0 + THINKING_CUE_DURATION_SEC + 0.01);
  osc.onended = () => {
    try {
      osc.disconnect();
      gain.disconnect();
    } catch (_err) {
      // ignore
    }
  };
  state.lastThinkingCueAtMs = Date.now();
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
    thinking: "",
    speaking: "",
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
