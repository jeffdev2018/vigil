"use client";

import { useState } from "react";
import { KeyRound, Copy, Check, Loader2 } from "lucide-react";
import { useSetAutopilotTriggerSigningSecret } from "@multica/core/autopilots";
import { Button } from "@multica/ui/components/ui/button";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { useT } from "../../i18n";
import type { AutopilotTrigger } from "@multica/core/types";

/** 32 random bytes as hex. The server floors the secret at 16 characters;
 *  this is well past that and matches what GitHub/Stripe mint. Generated on
 *  the client so the plaintext exists in exactly one place the user can copy
 *  before it becomes write-only. */
export function generateSigningSecret(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * Set, rotate or clear the HMAC secret a webhook trigger verifies deliveries
 * against. The value is shown ONCE, right after it is written: the API never
 * echoes a stored secret back, only the last four characters as a hint.
 */
export function SigningSecretSection({
  autopilotId,
  trigger,
}: {
  autopilotId: string;
  trigger: Pick<AutopilotTrigger, "id" | "has_signing_secret" | "signing_secret_hint">;
}) {
  const { t } = useT("autopilots");
  const setSecret = useSetAutopilotTriggerSigningSecret();
  // Local, deliberately not persisted: once this dialog closes the plaintext
  // is gone, which is the whole point of a write-only credential.
  const [minted, setMinted] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const configured = trigger.has_signing_secret === true || minted !== null;

  const write = async (value: string, successKey: "toast_rotated" | "toast_cleared") => {
    try {
      await setSecret.mutateAsync({
        autopilotId,
        triggerId: trigger.id,
        signingSecret: value,
      });
      setMinted(value === "" ? null : value);
      toast.success(t(($) => $.signing_secret[successKey]));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.signing_secret.toast_failed),
      );
    }
  };

  const handleCopy = async () => {
    if (minted === null) return;
    if (await copyText(minted)) {
      setCopied(true);
      toast.success(t(($) => $.signing_secret.copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.signing_secret.copy_failed));
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5 text-micro font-semibold tracking-[0.08em] text-muted-foreground uppercase">
        <KeyRound className="size-3" />
        {t(($) => $.signing_secret.label)}
      </div>

      <p className="text-caption text-muted-foreground leading-relaxed">
        {t(($) => $.signing_secret.hint)}
      </p>

      {minted !== null ? (
        // Shown once. After this dialog closes the value is unrecoverable —
        // the row only keeps the last four characters.
        <div className="space-y-1.5 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2">
          <div className="text-caption font-medium">
            {t(($) => $.signing_secret.shown_once)}
          </div>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 break-all font-mono text-caption">{minted}</code>
            <button
              type="button"
              onClick={handleCopy}
              title={t(($) => $.signing_secret.copy)}
              aria-label={t(($) => $.signing_secret.copy)}
              className="shrink-0 rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
            >
              {copied ? (
                <Check className="size-3.5 text-emerald-500" />
              ) : (
                <Copy className="size-3.5" />
              )}
            </button>
          </div>
        </div>
      ) : (
        <div className="text-caption text-muted-foreground">
          {configured && trigger.signing_secret_hint
            ? t(($) => $.signing_secret.configured, { hint: trigger.signing_secret_hint })
            : t(($) => $.signing_secret.not_configured)}
        </div>
      )}

      <div className="flex gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={setSecret.isPending}
          onClick={() => write(generateSigningSecret(), "toast_rotated")}
        >
          {setSecret.isPending && <Loader2 className="size-3.5 mr-1 animate-spin" />}
          {configured
            ? t(($) => $.signing_secret.rotate)
            : t(($) => $.signing_secret.generate)}
        </Button>
        {configured && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={setSecret.isPending}
            onClick={() => write("", "toast_cleared")}
          >
            {t(($) => $.signing_secret.clear)}
          </Button>
        )}
      </div>
    </div>
  );
}
