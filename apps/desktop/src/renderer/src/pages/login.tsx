import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";

function requireRuntimeAppUrl(): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config.appUrl;
}

function openWebLoginInBrowser(webUrl: string): void {
  // Open web login page in the default browser with platform=desktop flag.
  // The web callback will redirect back via multica:// deep link with the token.
  // Shared by Google and SSO: both funnel through the same web /login page,
  // which offers the same providers there as it does in-app.
  window.desktopAPI.openExternal(`${webUrl}/login?platform=desktop`);
}

export function DesktopLoginPage() {
  const webUrl = requireRuntimeAppUrl();
  const handleGoogleLogin = () => openWebLoginInBrowser(webUrl);
  const handleSsoLogin = () => openWebLoginInBrowser(webUrl);

  return (
    <div className="flex h-dvh flex-col">
      <DragStrip />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        onGoogleLogin={handleGoogleLogin}
        onSsoLogin={handleSsoLogin}
      />
    </div>
  );
}
