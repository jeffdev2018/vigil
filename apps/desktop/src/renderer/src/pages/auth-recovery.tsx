import { AuthRecoveryPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";

export function DesktopAuthRecoveryPage(props: {
  onRetry?: () => void;
  isRetrying?: boolean;
}) {
  return <AuthRecoveryPage {...props} topSlot={<DragStrip />} />;
}
