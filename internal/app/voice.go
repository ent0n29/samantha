package app

import (
	"fmt"
	"strings"

	"github.com/ent0n29/samantha/internal/config"
	"github.com/ent0n29/samantha/internal/voice"
)

type voiceSetup struct {
	sttProvider      voice.STTProvider
	ttsProvider      voice.TTSProvider
	resolvedProvider string
	defaultVoiceID   string
	defaultModelID   string
	detail           string
	cleanup          func() error
}

func resolveVoiceProviders(cfg config.Config) (voiceSetup, error) {
	voiceMode := strings.ToLower(strings.TrimSpace(cfg.VoiceProvider))
	if voiceMode == "" {
		voiceMode = "auto"
	}

	tryElevenLabs := func() (voiceSetup, bool) {
		if strings.TrimSpace(cfg.ElevenLabsAPIKey) == "" {
			return voiceSetup{}, false
		}
		p := voice.NewElevenLabsProvider(voice.ElevenLabsConfig{
			APIKey:              cfg.ElevenLabsAPIKey,
			WSBaseURL:           cfg.ElevenLabsWSBaseURL,
			STTModelID:          cfg.ElevenLabsSTTModel,
			STTCommitStrategy:   cfg.ElevenLabsSTTCommitStrategy,
			DefaultOutputFormat: cfg.ElevenLabsTTSOutputFormat,
		})
		return voiceSetup{
			sttProvider:      p,
			ttsProvider:      p,
			resolvedProvider: "elevenlabs",
			defaultVoiceID:   cfg.ElevenLabsTTSVoice,
			defaultModelID:   cfg.ElevenLabsTTSModel,
			detail:           "elevenlabs realtime",
			cleanup:          nil,
		}, true
	}

	tryLocal := func(fatal bool) (voiceSetup, bool, error) {
		p, err := voice.NewLocalProvider(voice.LocalConfig{
			WhisperCLI:         cfg.LocalWhisperCLI,
			WhisperModelPath:   cfg.LocalWhisperModelPath,
			WhisperLanguage:    cfg.LocalWhisperLanguage,
			WhisperThreads:     cfg.LocalWhisperThreads,
			WhisperBeamSize:    cfg.LocalWhisperBeamSize,
			WhisperBestOf:      cfg.LocalWhisperBestOf,
			KokoroPython:       cfg.LocalKokoroPython,
			KokoroWorkerScript: cfg.LocalKokoroWorkerScript,
			KokoroVoice:        cfg.LocalKokoroVoice,
			KokoroLangCode:     cfg.LocalKokoroLangCode,
		})
		if err != nil {
			if fatal {
				return voiceSetup{}, false, fmt.Errorf("local voice provider init failed: %w", err)
			}
			return voiceSetup{}, false, nil
		}

		defaultVoiceID := strings.TrimSpace(cfg.LocalKokoroVoice)
		if defaultVoiceID == "" {
			defaultVoiceID = "af_heart"
		}

		return voiceSetup{
			sttProvider:      p,
			ttsProvider:      p,
			resolvedProvider: "local",
			defaultVoiceID:   defaultVoiceID,
			defaultModelID:   "kokoro",
			detail:           fmt.Sprintf("local (%s + kokoro)", p.STTBackend()),
			cleanup:          p.Close,
		}, true, nil
	}

	switch voiceMode {
	case "elevenlabs":
		if setup, ok := tryElevenLabs(); ok {
			return setup, nil
		}
		// Be forgiving: if the user asked for ElevenLabs but has no key/credits,
		// fall back to local voice when available instead of failing hard.
		localSetup, hasLocal, err := tryLocal(false)
		if err != nil {
			return voiceSetup{}, err
		}
		if hasLocal {
			localSetup.resolvedProvider = "local"
			localSetup.detail = "local (elevenlabs unavailable)"
			return localSetup, nil
		}
		return voiceSetup{}, fmt.Errorf("VOICE_PROVIDER=elevenlabs but ELEVENLABS_API_KEY is not set (and local voice is unavailable)")
	case "local":
		setup, _, err := tryLocal(true)
		return setup, err
	case "mock":
		p := voice.NewMockProvider()
		return voiceSetup{
			sttProvider:      p,
			ttsProvider:      p,
			resolvedProvider: "mock",
			defaultVoiceID:   "",
			defaultModelID:   "",
			detail:           "mock",
			cleanup:          nil,
		}, nil
	case "auto":
		elevenSetup, hasEleven := tryElevenLabs()
		localSetup, hasLocal, err := tryLocal(false)
		if err != nil {
			return voiceSetup{}, err
		}

		if hasEleven && hasLocal {
			stt, tts := voice.NewFailoverProviderPair(
				elevenSetup.sttProvider,
				elevenSetup.ttsProvider,
				localSetup.sttProvider,
				localSetup.ttsProvider,
				localSetup.defaultVoiceID,
				localSetup.defaultModelID,
			)
			return voiceSetup{
				sttProvider:      stt,
				ttsProvider:      tts,
				resolvedProvider: "elevenlabs",
				defaultVoiceID:   elevenSetup.defaultVoiceID,
				defaultModelID:   elevenSetup.defaultModelID,
				detail:           "elevenlabs realtime (automatic local fallback)",
				cleanup:          localSetup.cleanup,
			}, nil
		}
		if hasEleven {
			return elevenSetup, nil
		}
		if hasLocal {
			return localSetup, nil
		}
		p := voice.NewMockProvider()
		return voiceSetup{
			sttProvider:      p,
			ttsProvider:      p,
			resolvedProvider: "mock",
			defaultVoiceID:   "",
			defaultModelID:   "",
			detail:           "mock (no elevenlabs key and local voice unavailable)",
			cleanup:          nil,
		}, nil
	default:
		return voiceSetup{}, fmt.Errorf("invalid VOICE_PROVIDER: %q (expected auto|elevenlabs|local|mock)", cfg.VoiceProvider)
	}
}
