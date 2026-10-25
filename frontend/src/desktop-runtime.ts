// Wails uses a custom scheme on macOS/Linux and its private localhost origin
// with WebView2 on Windows. A Vite/browser preview must keep browser behavior.
export function isDesktopRuntime(current: Pick<Location, "protocol" | "hostname"> = location): boolean {
  return current.protocol === "wails:" ||
    (["http:", "https:"].includes(current.protocol) && current.hostname === "wails.localhost");
}
