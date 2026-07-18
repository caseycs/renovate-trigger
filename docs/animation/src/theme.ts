// Flat, GIF-friendly palette (few colors, hard edges). Accept/ignore/reject
// hues mirror the classDef convention in WORKFLOWS.md §3.
export const theme = {
  bg: '#0d1117',
  panel: '#161b22',
  panelActive: '#1f2733',
  stroke: '#30363d',
  strokeActive: '#58a6ff',
  text: '#e6edf3',
  subtle: '#8b949e',
  accent: '#58a6ff', // blue — the app / in-flight
  accept: '#3fb950', // green — accepted / success
  warn: '#d29922', // yellow — ignore / postpone
  reject: '#f85149', // red — reject
  mono: '"SFMono-Regular", "JetBrains Mono", Menlo, Consolas, monospace',
  sans: '-apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
} as const;
