<script lang="ts">
  import type { Job } from "../lib/api";
  import { JobsService, errorMessage } from "../lib/api";
  import ImportEvidence from "../components/ImportEvidence.svelte";
  import { loadJobs, loadReviewCount, store } from "../lib/state.svelte";

  let importOpen = $state(false);
  let busyId = $state("");

  const jobs = $derived(store.jobs);
  const counts = $derived(store.jobCounts);

  function kindLabel(kind: string): string {
    switch (kind) {
      case "ingest.manual":
        return "Manual import";
      default:
        return kind;
    }
  }

  function statusLabel(status: string): string {
    switch (status) {
      case "queued":
        return "Queued";
      case "running":
        return "Running";
      case "succeeded":
        return "Succeeded";
      case "failed":
        return "Failed";
      case "cancelled":
        return "Cancelled";
      default:
        return status;
    }
  }

  function when(job: Job): string {
    const stamp = job.finishedAt || job.startedAt || job.createdAt;
    if (!stamp) return "—";
    const date = new Date(stamp);
    if (Number.isNaN(date.getTime())) return stamp;
    return date.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  function isActive(job: Job): boolean {
    return job.status === "queued" || job.status === "running";
  }

  async function cancel(job: Job) {
    busyId = job.id;
    try {
      await JobsService.Cancel(job.id);
      await loadJobs();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  async function retry(job: Job) {
    busyId = job.id;
    try {
      await JobsService.Retry(job.id);
      await loadJobs();
      await loadReviewCount();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Activity</h1>
      <p>Background imports and their progress. Jobs survive an app restart.</p>
    </div>
    <div class="row">
      <button class="btn" onclick={() => void loadJobs()}>Refresh</button>
      <button class="btn primary" onclick={() => (importOpen = true)}>+ Import text</button>
    </div>
  </header>

  <div class="stats">
    <article class="stat">
      <span class="label">Queued</span>
      <span class="value">{counts?.queued ?? 0}</span>
      <span class="hint">waiting for a worker</span>
    </article>
    <article class="stat accent">
      <span class="label">Running</span>
      <span class="value">{counts?.running ?? 0}</span>
      <span class="hint">parsing now</span>
    </article>
    <article class="stat">
      <span class="label">Succeeded</span>
      <span class="value">{counts?.succeeded ?? 0}</span>
      <span class="hint">finished imports</span>
    </article>
    <article class="stat">
      <span class="label">Failed</span>
      <span class="value">{counts?.failed ?? 0}</span>
      <span class="hint">{counts?.cancelled ?? 0} cancelled</span>
    </article>
  </div>

  {#if jobs.length === 0}
    <div class="card empty">
      <strong>No activity yet</strong>
      <span>Paste a receipt or email to start an import. The pipeline extracts details and queues them for review.</span>
      <button class="btn primary" onclick={() => (importOpen = true)}>Import text</button>
    </div>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Job</th>
            <th>Status</th>
            <th style="width: 22%;">Progress</th>
            <th>Message</th>
            <th>When</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each jobs as job (job.id)}
            <tr>
              <td>
                <div class="cell-title">{kindLabel(job.kind)}</div>
                <div class="cell-sub mono">{job.id.slice(-8)}</div>
              </td>
              <td><span class="badge {job.status}">{statusLabel(job.status)}</span></td>
              <td>
                <div class="job-progress">
                  <div class="job-progress-track">
                    <div
                      class="job-progress-fill"
                      class:failed={job.status === "failed"}
                      class:cancelled={job.status === "cancelled"}
                      style={`width: ${Math.max(2, Math.min(100, job.progress || (isActive(job) ? 4 : 100)))}%`}
                    ></div>
                  </div>
                  <span class="cell-sub mono">{job.progress || 0}%</span>
                </div>
              </td>
              <td>
                <div>{job.message || "—"}</div>
                {#if job.error}
                  <div class="cell-sub error-text">{job.error}</div>
                {/if}
              </td>
              <td class="cell-sub">{when(job)}</td>
              <td>
                <div class="row-actions">
                  {#if isActive(job)}
                    <button class="btn ghost sm" onclick={() => cancel(job)} disabled={busyId === job.id}>
                      Cancel
                    </button>
                  {:else if job.status === "failed" || job.status === "cancelled"}
                    <button class="btn ghost sm" onclick={() => retry(job)} disabled={busyId === job.id}>
                      Retry
                    </button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<ImportEvidence open={importOpen} onclose={() => (importOpen = false)} onqueued={() => void loadJobs()} />
