/** Reply of POST /api/voice/transcribe: the spoken audio as text. */
/** Connection details for the provider's realtime transcription WebSocket. */
export interface RealtimeVoiceSession {
  url: string;
  model: string;
  /** Short-lived provider token, sent as a WebSocket sub-protocol. */
  token: string;
  expires_at: string;
  encoding: string;
  sample_rate: number;
}

export interface VoiceTranscription {
  text: string;
}
