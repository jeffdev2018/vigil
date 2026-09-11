import { useEffect, useState } from "react";

/**
 * A counter that bumps whenever the document's theme may have changed: a
 * class/style/data-theme change on <html> or <body>, or an OS color-scheme
 * switch. Observing the DOM rather than the theme provider's state matters:
 * the provider applies the class in its own effect, which runs after a child's
 * effect, so a child keyed on the provider's state would read stale tokens.
 */
export function useThemeVersion() {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    const bumpThemeVersion = () => setThemeVersion((version) => version + 1);
    const observer = new MutationObserver(bumpThemeVersion);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["class", "style", "data-theme"],
      });
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", bumpThemeVersion);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener("change", bumpThemeVersion);
    };
  }, []);

  return themeVersion;
}
