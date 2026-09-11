/**
 * User-facing protocol names shared by every MCP inventory and editor.
 * STDIO / Streamable HTTP / SSE are protocol names, not prose — left
 * untranslated on purpose, same as "HTTP" or "TCP" would be. `unknownLabel`
 * covers only the one genuinely-English fallback string (an unrecognized
 * transport), and defaults to the bare English word for callers that have
 * not wired a translated one through yet.
 */
export function mcpTransportLabel(transport: string, unknownLabel = "Unknown"): string {
  const normalized = transport.trim().toLowerCase();
  switch (normalized) {
    case "local":
    case "stdio":
      return "STDIO";
    case "remote":
    case "http":
    case "streamable-http":
      return "Streamable HTTP";
    case "sse":
      return "SSE";
    default:
      return transport.trim() || unknownLabel;
  }
}
