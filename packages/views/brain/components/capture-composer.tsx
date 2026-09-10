"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Mic, Paperclip, Send, Square } from "lucide-react";
import { ApiError } from "@multica/core/api";
import type { BrainCapture } from "@multica/core/types";
import { useCaptureText, useCaptureUpload } from "@multica/core/brain/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * An upload posts through the web app's Next.js `/api` rewrite, which stalls
 * on request bodies past this and answers 500 after ~30s (8 MB passes, 16 MB
 * hangs). The server itself accepts 50 MB, so a bigger file is not refused —
 * it is refused *here*, and pointed at the CLI, which talks to the API
 * directly. Same guard as the pack upload in settings.
 */
const UPLOAD_MAX_BYTES = 8 * 1024 * 1024;
const UPLOAD_MAX_LABEL = `${UPLOAD_MAX_BYTES / 1024 / 1024} MB`;

/** A pasted or typed lone http(s) URL is a link capture, not a text one. */
const LONE_URL = /^https?:\/\/\S+$/i;

export function isLoneUrl(text: string): boolean {
  return LONE_URL.test(text.trim());
}

/** The server's own text, when there is one, shown under the translated label. */
function errorDetail(error: unknown): string {
  if (error instanceof ApiError && error.status === 503) return "";
  return error instanceof Error && error.message ? error.message : "";
}

/**
 * File and voice-memo capture. Lives here (not in the inbox) because three
 * entry points share it — the attach button, a drop on the inbox, and the
 * recorder — and all three need the same size guard and the same error line.
 */
export function useFileCapture(wsId: string) {
  const { t } = useT("brain");
  const upload = useCaptureUpload(wsId);
  const [error, setError] = useState("");

  const capture = useCallback(
    async (file: File | Blob, titleHint?: string): Promise<BrainCapture | null> => {
      setError("");
      if (file.size > UPLOAD_MAX_BYTES) {
        setError(t(($) => $.capture.too_large, { size: UPLOAD_MAX_LABEL }));
        return null;
      }
      try {
        return await upload.mutateAsync({ file, title_hint: titleHint });
      } catch (err) {
        setError(
          err instanceof ApiError && err.status === 503
            ? t(($) => $.capture.storage_off)
            : t(($) => $.capture.error),
        );
        return null;
      }
    },
    [t, upload],
  );

  return {
    capture,
    isPending: upload.isPending,
    error,
    detail: errorDetail(upload.error),
    clearError: () => setError(""),
  };
}

/** MediaRecorder is absent in jsdom and in older browsers; the button hides. */
function recordingSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.MediaRecorder !== "undefined" &&
    typeof navigator !== "undefined" &&
    typeof navigator.mediaDevices?.getUserMedia === "function"
  );
}

function formatElapsed(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export function CaptureComposer({
  wsId,
  file,
}: {
  wsId: string;
  file: ReturnType<typeof useFileCapture>;
}) {
  const { t } = useT("brain");
  const capture = useCaptureText(wsId);

  const [text, setText] = useState("");
  const [todo, setTodo] = useState(false);
  const [error, setError] = useState("");
  const [recording, setRecording] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [canRecord, setCanRecord] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);

  // Support is a client fact: deciding it during render would make the server
  // and the first client paint disagree.
  useEffect(() => setCanRecord(recordingSupported()), []);

  useEffect(() => {
    if (!recording) return;
    const id = setInterval(() => setElapsed((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [recording]);

  const submitText = useCallback(
    async (raw: string) => {
      const content = raw.trim();
      if (content === "") return;
      setError("");
      try {
        await capture.mutateAsync(
          isLoneUrl(content)
            ? { url: content, kind: todo ? "todo" : undefined }
            : { content, kind: todo ? "todo" : "text" },
        );
        setText("");
        setTodo(false);
      } catch {
        setError(t(($) => $.capture.error));
      }
    },
    [capture, t, todo],
  );

  const startRecording = useCallback(async () => {
    setError("");
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const chunks: Blob[] = [];
      const recorder = new MediaRecorder(stream, { mimeType: "audio/webm" });
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunks.push(event.data);
      };
      recorder.onstop = () => {
        for (const track of stream.getTracks()) track.stop();
        setRecording(false);
        const blob = new Blob(chunks, { type: "audio/webm" });
        if (blob.size > 0) void file.capture(blob, `memo-${Date.now()}`);
      };
      recorder.start();
      recorderRef.current = recorder;
      setElapsed(0);
      setRecording(true);
    } catch {
      // Permission refused, or no microphone. Say so; never fail silently.
      setError(t(($) => $.capture.error));
    }
  }, [file, t]);

  const busy = capture.isPending || file.isPending;

  return (
    <div className="flex shrink-0 flex-col gap-2 border-b px-4 py-3">
      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            void submitText(text);
          }
        }}
        onPaste={(e) => {
          const pasted = e.clipboardData.getData("text");
          // A pasted URL into an empty box is a link capture: no Enter needed.
          if (text.trim() === "" && isLoneUrl(pasted)) {
            e.preventDefault();
            void submitText(pasted);
          }
        }}
        aria-label={t(($) => $.capture.label)}
        placeholder={t(($) => $.capture.placeholder)}
        className="min-h-20 resize-none text-body"
      />

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          aria-pressed={todo}
          onClick={() => setTodo(!todo)}
          title={t(($) => $.capture.todo_hint)}
          className={cn(
            "rounded-md border px-2 py-0.5 text-caption transition-colors",
            // The pressed state carries weight and text colour, which hover
            // does not touch, so a hovered "on" toggle still reads as on.
            todo
              ? "border-primary/40 bg-accent font-medium text-foreground"
              : "text-muted-foreground hover:bg-accent",
          )}
        >
          {t(($) => $.capture.todo)}
        </button>

        <input
          ref={inputRef}
          type="file"
          className="hidden"
          data-testid="capture-file-input"
          aria-label={t(($) => $.capture.attach)}
          onChange={(e) => {
            const picked = e.target.files?.[0];
            e.target.value = "";
            if (picked) void file.capture(picked);
          }}
        />
        <Button
          variant="ghost"
          size="sm"
          disabled={busy}
          onClick={() => inputRef.current?.click()}
        >
          <Paperclip aria-hidden="true" className="size-3.5" />
          {t(($) => $.capture.attach)}
        </Button>

        {canRecord ? (
          <Button
            variant={recording ? "destructive" : "ghost"}
            size="sm"
            disabled={busy && !recording}
            onClick={() => {
              if (recording) recorderRef.current?.stop();
              else void startRecording();
            }}
          >
            {recording ? (
              <>
                <Square aria-hidden="true" className="size-3.5" />
                {t(($) => $.capture.stop)}
              </>
            ) : (
              <>
                <Mic aria-hidden="true" className="size-3.5" />
                {t(($) => $.capture.record)}
              </>
            )}
          </Button>
        ) : null}
        {recording ? (
          <span role="status" className="text-caption text-muted-foreground">
            {t(($) => $.capture.recording, { elapsed: formatElapsed(elapsed) })}
          </span>
        ) : null}

        <Button
          size="sm"
          className="ml-auto"
          disabled={busy || text.trim() === ""}
          onClick={() => void submitText(text)}
        >
          {busy ? (
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
          ) : (
            <Send aria-hidden="true" className="size-3.5" />
          )}
          {capture.isPending
            ? t(($) => $.capture.capturing)
            : file.isPending
              ? t(($) => $.capture.uploading)
              : t(($) => $.capture.submit)}
        </Button>
      </div>

      {error || file.error ? (
        <div role="alert" className="flex flex-col gap-0.5">
          <p className="text-caption text-destructive">{error || file.error}</p>
          {file.detail ? (
            <p className="break-words text-caption text-muted-foreground">
              {file.detail}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
