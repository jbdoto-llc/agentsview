<script lang="ts">
  import { Button, EmptyState, SearchInput } from "@kenn-io/kit-ui";
  import { onDestroy } from "svelte";
  import { m } from "../../i18n/index.js";
  import { FleetService } from "../../api/generated/index";
  import type {
    FleetStoriesResponse,
    FleetStoryResponse,
  } from "../../api/generated/models/index";
  import { isAbortError } from "../../api/runtime.js";
  import { router } from "../../stores/router.svelte.js";
  import { formatDuration } from "../../utils/duration.js";
  import { formatRelativeTime } from "../../utils/format.js";
  import { LatestRead } from "../../utils/latest-read.js";
  import { formatUSD, prLabel, stateTone, totalTokens, traceHref } from "./stories.js";

  let list: FleetStoriesResponse | null = $state(null);
  let detail: FleetStoryResponse | null = $state(null);
  let loading = $state(true);
  let failed = $state(false);
  const read = new LatestRead();

  const PAGE_SIZE = 50;

  const storyId = $derived(router.params.id ?? "");
  const query = $derived(router.params.q ?? "");
  const page = $derived(Math.max(1, Number.parseInt(router.params.page ?? "1", 10) || 1));

  // The search box leads the URL by one debounce; it follows the URL when
  // history moves it (back/forward).
  let search = $state("");
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let pageEl: HTMLDivElement | undefined = $state();
  $effect(() => {
    search = query;
  });

  $effect(() => {
    void load(storyId, query, page);
  });

  function listParams(q: string, p: number): Record<string, string> {
    const params: Record<string, string> = {};
    if (q) params.q = q;
    if (p > 1) params.page = String(p);
    return params;
  }

  function scheduleSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => router.replace("stories", listParams(search.trim(), 1)), 250);
  }

  function goToPage(p: number) {
    router.navigate("stories", listParams(query, p));
    pageEl?.scrollIntoView({ block: "start" });
  }

  async function load(id: string, q: string, p: number) {
    const signal = read.begin();
    loading = true;
    failed = false;
    try {
      if (id) {
        const res = await FleetService.getApiV1FleetStoriesById({ id }, { signal });
        if (!read.isCurrent(signal)) return;
        detail = res;
      } else {
        const res = await FleetService.getApiV1FleetStories(
          { q: q || undefined, offset: (p - 1) * PAGE_SIZE, limit: PAGE_SIZE },
          { signal },
        );
        if (!read.isCurrent(signal)) return;
        list = res;
      }
    } catch (e) {
      if (isAbortError(e) || !read.isCurrent(signal)) return;
      failed = true;
    } finally {
      if (read.finish(signal)) loading = false;
    }
  }

  onDestroy(() => {
    read.cancel();
    clearTimeout(searchTimer);
  });

  function openStory(e: MouseEvent, id: string) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    router.navigate("stories", { id });
  }

  function backToList(e: MouseEvent) {
    e.preventDefault();
    router.navigate("stories");
  }

  function roleLabel(role: string | null | undefined): string {
    switch (role) {
      case "worker":
        return m.stories_role_worker();
      case "review":
        return m.stories_role_review();
      default:
        return role ?? "";
    }
  }

  function sessionRole(runs: { session_id?: string; role?: string }[], id: string): string {
    return roleLabel(runs.find((r) => r.session_id === id)?.role);
  }

  function stateLabel(state: string): string {
    switch (state) {
      case "running":
      case "claimed":
        return m.stories_state_running();
      case "pr_open":
        return m.stories_state_pr_open();
      case "failed":
        return m.stories_state_failed();
      case "closed":
        return m.stories_state_closed();
      case "done":
      case "ok":
        return m.stories_state_done();
      default:
        return state;
    }
  }
</script>

<div class="stories-page" bind:this={pageEl}>
  {#if storyId}
    <a class="back-link" href={router.buildHref("stories")} onclick={backToList}>← {m.stories_back()}</a>
  {/if}

  {#if loading && !list && !detail}
    <div class="loading-state">{m.stories_loading()}</div>
  {:else if failed}
    <EmptyState title={m.stories_error()} />
  {:else if storyId && detail}
    {@const story = detail.story}
    <header class="story-header">
      <div class="story-id">{story.id}</div>
      <h2>{story.title}</h2>
      <div class="story-meta">
        <span class="badge tone-{stateTone(story.state)}">{stateLabel(story.state)}</span>
        <span>{m.stories_col_attempts()}: {story.attempts}</span>
        <span>{m.stories_col_cost()}: {formatUSD(story.cost_usd)}</span>
        {#if story.pr_url}
          <a href={story.pr_url} target="_blank" rel="noopener noreferrer">{m.stories_col_pr()} {prLabel(story.pr_url)}</a>
        {/if}
        {#each story.labels as label (label)}
          <span class="label">{label}</span>
        {/each}
      </div>
    </header>

    <section>
      <h3>{m.stories_sessions()}</h3>
      {#if story.sessions.length === 0}
        <p class="muted">—</p>
      {:else}
        <ul class="session-list">
          {#each story.sessions as session (session.id)}
            {@const role = sessionRole(story.runs, session.id)}
            <li>
              {#if role}<span class="label">{role}</span>{/if}
              {#if session.exists}
                <a href={router.buildSessionHref(session.id)}>{session.display_name || session.id}</a>
                <span class="muted">{session.machine} · {session.project}</span>
              {:else}
                <span class="mono">{session.id}</span>
                <span class="muted">{m.stories_session_missing()}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section>
      <h3>{m.stories_runs()}</h3>
      <div class="run-list">
        {#each story.runs as run, i (run.id || i)}
          <div class="run-card">
            <div class="run-head">
              <span class="badge tone-{stateTone(run.status)}">{stateLabel(run.status)}</span>
              <span class="mono">{run.id || "—"}</span>
              {#if run.started_at}<span class="muted">{formatRelativeTime(run.started_at)}</span>{/if}
            </div>
            <dl class="run-facts">
              {#if run.role}<dt>{m.stories_role()}</dt><dd>{roleLabel(run.role)}</dd>{/if}
              {#if run.duration_s}<dt>{m.stories_duration()}</dt><dd>{formatDuration(run.duration_s * 1000)}</dd>{/if}
              {#if run.turns}<dt>{m.stories_turns()}</dt><dd>{run.turns}</dd>{/if}
              {#if run.cost_usd}<dt>{m.stories_col_cost()}</dt><dd>{formatUSD(run.cost_usd)}</dd>{/if}
              {#if totalTokens(run.tokens)}<dt>{m.stories_tokens()}</dt><dd>{totalTokens(run.tokens).toLocaleString()}</dd>{/if}
              {#if run.model}<dt>{m.stories_model()}</dt><dd>{run.model}</dd>{/if}
              {#if run.reason}<dt>{m.stories_reason()}</dt><dd>{run.reason}</dd>{/if}
            </dl>
            <div class="run-links">
              {#if run.pr_url}<a href={run.pr_url} target="_blank" rel="noopener noreferrer">{m.stories_col_pr()} {prLabel(run.pr_url)}</a>{/if}
              {#if traceHref(detail.trace_url_template, run.trace_id)}
                <a href={traceHref(detail.trace_url_template, run.trace_id)} target="_blank" rel="noopener noreferrer">{m.stories_trace()}</a>
              {:else if run.trace_id}
                <span class="muted">{m.stories_trace()}: <span class="mono">{run.trace_id}</span></span>
              {/if}
              {#if run.transcript}<span class="muted">{m.stories_transcript()}: <span class="mono">{run.transcript}</span></span>{/if}
            </div>
          </div>
        {/each}
      </div>
    </section>

    <section>
      <h3>{m.stories_timeline()}</h3>
      <ol class="timeline">
        {#each story.events ?? [] as event, i (i)}
          <li>
            <span class="muted">{formatRelativeTime(event.at)}</span>
            <span class="mono">{event.event}</span>
            {#if event.role}<span class="muted">{event.role}</span>{/if}
          </li>
        {/each}
      </ol>
    </section>
  {:else if list}
    <header class="stories-header">
      <h2>{m.stories_title()}</h2>
      <p class="muted">{m.stories_subtitle()}</p>
    </header>
    {#if !list.enabled}
      <EmptyState title={m.stories_not_configured()} />
    {:else}
      <SearchInput
        class="stories-search"
        bind:value={search}
        oninput={scheduleSearch}
        placeholder={m.stories_search_placeholder()}
        ariaLabel={m.stories_search_placeholder()}
        clearLabel={m.stories_search_clear()}
        block
      />
      {#if list.stories.length === 0}
        <EmptyState title={query ? m.stories_no_matches() : m.stories_empty()} />
      {:else}
      <table class="stories-table">
        <thead>
          <tr>
            <th>{m.stories_col_story()}</th>
            <th>{m.stories_col_state()}</th>
            <th class="num">{m.stories_col_attempts()}</th>
            <th class="num">{m.stories_col_cost()}</th>
            <th>{m.stories_col_activity()}</th>
            <th>{m.stories_col_pr()}</th>
          </tr>
        </thead>
        <tbody>
          {#each list.stories as story (story.ledger + story.id)}
            <tr>
              <td>
                <a href={router.buildHref("stories", { id: story.id })} onclick={(e) => openStory(e, story.id)}>
                  <span class="mono">{story.id}</span>
                  <span class="story-title">{story.title}</span>
                </a>
              </td>
              <td><span class="badge tone-{stateTone(story.state)}">{stateLabel(story.state)}</span></td>
              <td class="num">{story.attempts}</td>
              <td class="num">{formatUSD(story.cost_usd)}</td>
              <td class="muted">{formatRelativeTime(story.last_event)}</td>
              <td>
                {#if story.pr_url}
                  <a href={story.pr_url} target="_blank" rel="noopener noreferrer">{prLabel(story.pr_url)}</a>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      {@const start = (page - 1) * PAGE_SIZE + 1}
      <nav class="pager">
        <span class="muted">
          {m.stories_page_range({ start, end: start + list.stories.length - 1, total: list.total })}
        </span>
        <Button size="sm" surface="soft" label={m.stories_page_previous()} disabled={page <= 1} onclick={() => goToPage(page - 1)} />
        <Button
          size="sm"
          surface="soft"
          label={m.stories_page_next()}
          disabled={start + list.stories.length - 1 >= list.total}
          onclick={() => goToPage(page + 1)}
        />
      </nav>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .stories-page {
    max-width: 1000px;
    margin: 0 auto;
    padding: 32px 24px;
  }

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }

  h3 {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    margin: 24px 0 8px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .muted {
    color: var(--text-muted);
  }

  .mono {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 12px;
  }

  .loading-state {
    text-align: center;
    color: var(--text-muted);
    padding: 40px 0;
    font-size: 13px;
  }

  .stories-header p {
    margin: 4px 0 16px;
    font-size: 13px;
  }

  .stories-page :global(.stories-search) {
    margin-bottom: 12px;
  }

  .pager {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
    font-size: 13px;
  }

  .stories-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .stories-table th {
    text-align: left;
    font-weight: 500;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-default);
    padding: 6px 8px;
  }

  .stories-table td {
    border-bottom: 1px solid var(--border-muted);
    padding: 8px;
    vertical-align: middle;
  }

  .stories-table .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .stories-table a {
    display: flex;
    flex-direction: column;
    gap: 2px;
    color: var(--text-primary);
    text-decoration: none;
  }

  .stories-table a:hover .story-title {
    text-decoration: underline;
  }

  .badge {
    display: inline-block;
    font-size: 11px;
    font-weight: 600;
    padding: 1px 8px;
    border-radius: 10px;
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--text-muted) 14%, transparent);
  }

  .badge.tone-running {
    color: var(--accent-blue, #3b82f6);
    background: color-mix(in srgb, var(--accent-blue, #3b82f6) 14%, transparent);
  }

  .badge.tone-ok {
    color: var(--accent-green, #16a34a);
    background: color-mix(in srgb, var(--accent-green, #16a34a) 14%, transparent);
  }

  .badge.tone-failed {
    color: var(--accent-red, #e55);
    background: color-mix(in srgb, var(--accent-red, #e55) 14%, transparent);
  }

  .back-link {
    display: inline-block;
    font-size: 13px;
    color: var(--text-secondary);
    text-decoration: none;
    margin-bottom: 12px;
  }

  .story-id {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 12px;
    color: var(--text-muted);
  }

  .story-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-secondary);
  }

  .label {
    font-size: 11px;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    padding: 0 6px;
  }

  .session-list,
  .timeline {
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: 13px;
  }

  .session-list li,
  .timeline li {
    display: flex;
    gap: 10px;
    padding: 4px 0;
  }

  .run-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .run-card {
    background: var(--bg-surface);
    border: 1px solid var(--border-muted);
    border-radius: 8px;
    padding: 12px 14px;
  }

  .run-head {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
  }

  .run-facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 2px 12px;
    margin: 10px 0 0;
    font-size: 13px;
  }

  .run-facts dt {
    color: var(--text-muted);
  }

  .run-facts dd {
    margin: 0;
  }

  .run-links {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 8px;
    font-size: 13px;
  }
</style>
