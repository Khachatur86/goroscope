import { useEffect, useRef } from "react";

type PlaySpeed = 1 | 2 | 4;

interface UsePlaybackOptions {
  isPlaying: boolean;
  playSpeed: PlaySpeed;
  traceMinNS: number;
  traceMaxNS: number;
  scrubTimeNS: number | null;
  onScrubChange: (ns: number) => void;
  onStop: () => void;
}

export function usePlayback({
  isPlaying,
  playSpeed,
  traceMinNS,
  traceMaxNS,
  scrubTimeNS,
  onScrubChange,
  onStop,
}: UsePlaybackOptions) {
  const playbackRef = useRef<{ rafId: number; lastWall: number; traceNS: number } | null>(null);

  useEffect(() => {
    if (!isPlaying || traceMaxNS <= traceMinNS) {
      if (playbackRef.current) {
        cancelAnimationFrame(playbackRef.current.rafId);
        playbackRef.current = null;
      }
      return;
    }
    const startTrace =
      scrubTimeNS !== null && scrubTimeNS > traceMinNS && scrubTimeNS < traceMaxNS
        ? scrubTimeNS
        : traceMinNS;

    const state = { rafId: 0, lastWall: performance.now(), traceNS: startTrace };
    playbackRef.current = state;

    const tick = (now: number) => {
      const elapsed = now - state.lastWall;
      state.lastWall = now;
      state.traceNS += elapsed * 1e6 * playSpeed;
      if (state.traceNS >= traceMaxNS) {
        onScrubChange(traceMaxNS);
        onStop();
        return;
      }
      onScrubChange(state.traceNS);
      state.rafId = requestAnimationFrame(tick);
      playbackRef.current = state;
    };
    state.rafId = requestAnimationFrame(tick);
    playbackRef.current = state;

    return () => {
      cancelAnimationFrame(state.rafId);
      playbackRef.current = null;
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPlaying, playSpeed, traceMinNS, traceMaxNS]);
}
