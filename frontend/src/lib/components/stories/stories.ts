/** Formatting helpers for the fleet Stories page. */

/** Dollars with two decimals; tiny non-zero amounts read as "<$0.01". */
export function formatUSD(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value) || value <= 0) return "$0.00";
  if (value < 0.01) return "<$0.01";
  return `$${value.toFixed(2)}`;
}

/** A run's trace link from the configured template, or null. */
export function traceHref(
  template: string | null | undefined,
  traceId: string | null | undefined,
): string | null {
  if (!template || !traceId || !template.includes("{trace_id}")) return null;
  return template.split("{trace_id}").join(encodeURIComponent(traceId));
}

/** "#552" for a pull request URL, otherwise the URL itself. */
export function prLabel(url: string | null | undefined): string {
  if (!url) return "";
  const match = /\/pull\/(\d+)(?:[/?#]|$)/.exec(url);
  return match ? `#${match[1]}` : url;
}

export type StateTone = "running" | "ok" | "failed" | "muted";

/** Visual tone for a story or run state. */
export function stateTone(state: string | null | undefined): StateTone {
  switch (state) {
    case "running":
    case "claimed":
      return "running";
    case "pr_open":
    case "done":
    case "ok":
    case "closed":
      return "ok";
    case "failed":
      return "failed";
    default:
      return "muted";
  }
}

/** Total tokens across kinds. */
export function totalTokens(
  t:
    | {
        input?: number;
        output?: number;
        cache_read?: number;
        cache_creation?: number;
      }
    | null
    | undefined,
): number {
  if (!t) return 0;
  return (t.input ?? 0) + (t.output ?? 0) + (t.cache_read ?? 0) + (t.cache_creation ?? 0);
}
