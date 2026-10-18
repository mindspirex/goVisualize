"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import Viewer from "@/components/viewer";

type Repository = {
  owner: string;
  repo: string;
  sha: string;
  modules: string[];
};

type Job = {
  id: string;
  status: "queued" | "running" | "complete" | "error";
  error?: string;
};

type Inspection = Job & {
  repository?: Repository;
  downloaded?: number;
  total?: number;
};

type Stage =
  | "idle"
  | "inspecting"
  | "choosing"
  | "starting"
  | "running"
  | "complete"
  | "error";

const buttonClass =
  "min-h-10 cursor-pointer rounded-md border border-accent bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-[#105e52] disabled:cursor-wait disabled:opacity-60";
const quietButtonClass =
  "min-h-9 cursor-pointer rounded-md border border-line bg-surface px-3 py-2 text-xs text-ink transition-colors hover:border-accent hover:bg-accent-light";

async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`/api/github/${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
  });
  const result = await response.json();
  if (!response.ok) {
    throw new Error(result.error ?? "The request failed. Try again.");
  }
  return result as T;
}

function moduleLabel(module: string) {
  return module === "." ? "Repository root" : module;
}

export default function GitHubExplorer() {
  const [url, setURL] = useState("");
  const [stage, setStage] = useState<Stage>("idle");
  const [repository, setRepository] = useState<Repository | null>(null);
  const [modulePath, setModulePath] = useState("");
  const [inspectionID, setInspectionID] = useState("");
  const [jobID, setJobID] = useState("");
  const [downloaded, setDownloaded] = useState(0);
  const [downloadTotal, setDownloadTotal] = useState(0);
  const [error, setError] = useState("");
  const indeterminateProgress = useRef<HTMLDivElement>(null);
  const requestVersion = useRef(0);
  const preferredModuleRef = useRef("");
  const initialized = useRef(false);

  async function begin(repo: Repository, module: string, version: number, inspection: string) {
    setModulePath(module);
    setStage("starting");
    setDownloaded(0);
    setDownloadTotal(0);
    try {
      const job = await api<Job>("analyses", {
        inspectionId: inspection,
        owner: repo.owner,
        repo: repo.repo,
        sha: repo.sha,
        module,
      });
      if (version !== requestVersion.current) return;
      setJobID(job.id);
      const query = new URLSearchParams({
        repo: `${repo.owner}/${repo.repo}`,
        module,
      });
      window.history.replaceState(null, "", `/?${query}`);
      setStage(job.status === "complete" ? "complete" : "running");
    } catch (cause) {
      if (version !== requestVersion.current) return;
      setError(cause instanceof Error ? cause.message : "Could not start analysis.");
      setStage("error");
    }
  }

  async function inspect(value: string, preferredModule = "") {
    const version = ++requestVersion.current;
    preferredModuleRef.current = preferredModule;
    setURL(value);
    setRepository(null);
    setInspectionID("");
    setJobID("");
    setDownloaded(0);
    setDownloadTotal(0);
    setError("");
    setStage("inspecting");
    try {
      const job = await api<Inspection>("repositories", { url: value });
      if (version !== requestVersion.current) return;
      setInspectionID(job.id);
    } catch (cause) {
      if (version !== requestVersion.current) return;
      setError(cause instanceof Error ? cause.message : "Could not load repository.");
      setStage("error");
    }
  }

  async function finishInspection(repo: Repository, version: number, inspection: string) {
    if (version !== requestVersion.current) return;
    setRepository(repo);
    const module = preferredModuleRef.current;
    if (module && !repo.modules.includes(module)) {
      setError(`Module ${module} was not found at this commit.`);
      setStage("error");
    } else if (module || repo.modules.length === 1) {
      await begin(repo, module || repo.modules[0], version, inspection);
    } else {
      setStage("choosing");
    }
  }

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    const query = new URLSearchParams(window.location.search);
    const repo = query.get("repo");
    if (repo) {
      void inspect(`https://github.com/${repo}`, query.get("module") ?? "");
    }
  }, []);

  useEffect(() => {
    function restoreFromURL() {
      const query = new URLSearchParams(window.location.search);
      const repo = query.get("repo");
      if (repo) {
        void inspect(`https://github.com/${repo}`, query.get("module") ?? "");
        return;
      }
      requestVersion.current++;
      setURL("");
      setStage("idle");
      setRepository(null);
      setModulePath("");
      setInspectionID("");
      setJobID("");
      setError("");
    }
    window.addEventListener("popstate", restoreFromURL);
    return () => window.removeEventListener("popstate", restoreFromURL);
  }, []);

  useEffect(() => {
    if (stage !== "inspecting" || !inspectionID) return;
    let active = true;
    let pending = false;
    const version = requestVersion.current;
    const timer = window.setInterval(async () => {
      if (pending) return;
      pending = true;
      try {
        const job = await api<Inspection>(`repositories/${inspectionID}`);
        if (!active || version !== requestVersion.current) return;
        if (job.downloaded !== undefined) setDownloaded(job.downloaded);
        if (job.total !== undefined && job.total > 0) setDownloadTotal(job.total);
        if (job.status === "complete") {
          if (!job.repository) throw new Error("Repository inspection returned no modules.");
          void finishInspection(job.repository, version, inspectionID);
        }
        if (job.status === "error") {
          setError(job.error ?? "Could not inspect repository.");
          setStage("error");
        }
      } catch (cause) {
        if (!active || version !== requestVersion.current) return;
        setError(cause instanceof Error ? cause.message : "Could not check repository status.");
        setStage("error");
      } finally {
        pending = false;
      }
    }, 1200);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [inspectionID, stage]);

  useEffect(() => {
    if (stage !== "running" || !jobID) return;
    let active = true;
    let pending = false;
    const timer = window.setInterval(async () => {
      if (pending) return;
      pending = true;
      try {
        const job = await api<Job>(`analyses/${jobID}`);
        if (!active) return;
        if (job.status === "complete") setStage("complete");
        if (job.status === "error") {
          setError(job.error ?? "Analysis failed.");
          setStage("error");
        }
      } catch (cause) {
        if (!active) return;
        setError(cause instanceof Error ? cause.message : "Could not check analysis status.");
        setStage("error");
      } finally {
        pending = false;
      }
    }, 1200);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [jobID, stage]);

  function startOver() {
    requestVersion.current++;
    setURL("");
    setStage("idle");
    setRepository(null);
    setModulePath("");
    setInspectionID("");
    setJobID("");
    setError("");
    window.history.pushState(null, "", "/");
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (url.trim()) void inspect(url.trim());
  }

  const downloadPercent = stage === "inspecting" && downloadTotal > 0
    ? Math.max(0, Math.min(100, (downloaded / downloadTotal) * 100))
    : undefined;

  useEffect(() => {
    const segment = indeterminateProgress.current;
    if (!segment || downloadPercent !== undefined) return;

    const animation = segment.animate(
      [{ transform: "translateX(-100%)" }, { transform: "translateX(400%)" }],
      { duration: 1600, easing: "ease-in-out", iterations: Infinity },
    );
    return () => animation.cancel();
  }, [downloadPercent, stage]);

  if (stage === "complete" && repository && jobID) {
    return (
      <div className="flex h-dvh flex-col overflow-hidden">
        <header className="flex min-h-16 shrink-0 flex-wrap items-center justify-between gap-3 border-b border-line bg-surface px-6 py-3">
          <div className="min-w-0">
            <div className="truncate font-mono text-sm font-semibold" title={`${repository.owner}/${repository.repo}`}>
              {repository.owner}/{repository.repo}
            </div>
            <div className="mt-0.5 truncate font-mono text-[11px] text-muted">
              {moduleLabel(modulePath)} · {repository.sha.slice(0, 8)}
            </div>
          </div>
          <div className="flex shrink-0 gap-2">
            <button className={quietButtonClass} onClick={startOver}>
              New repository
            </button>
          </div>
        </header>
        <Viewer treeUrl={`/api/github/graphs/${jobID}`} />
      </div>
    );
  }

  return (
    <main className="flex min-h-dvh items-center justify-center px-4 py-12">
      <div className="w-full max-w-[680px]">
        <h1 className="max-w-[18ch] text-[clamp(2rem,5vw,3.5rem)] font-semibold leading-[1.1] tracking-[-0.03em]">
          Explore a Go project
        </h1>
        <p className="mt-5 max-w-[62ch] text-base leading-relaxed text-muted">
          Enter a public GitHub repository to see its default branch function calls as a tree.
        </p>
        <form className="mt-9" onSubmit={submit}>
          <label className="block text-sm font-medium" htmlFor="repository-url">
            GitHub repository
          </label>
          <div className="mt-3 flex gap-3">
            <input
              id="repository-url"
              className="min-h-11 min-w-0 flex-1 rounded-md border border-line bg-surface px-3 font-mono text-sm text-ink placeholder:text-muted focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-accent"
              placeholder="https://github.com/owner/repo"
              autoComplete="url"
              spellCheck={false}
              value={url}
              onChange={(event) => setURL(event.target.value)}
              required
            />
            <button className={buttonClass} disabled={stage === "inspecting" || stage === "starting" || stage === "running"}>
              Explore repository
            </button>
          </div>
        </form>
        {error && (
          <p className="mt-5 rounded-md bg-[#fff1e9] px-4 py-3 text-sm text-[#804224]" role="alert">
            {error}
          </p>
        )}
        {(stage === "inspecting" || stage === "starting" || stage === "running") && (
          <div className="mt-8">
            <div
              className="relative h-1.5 overflow-hidden rounded-full bg-line"
              role="progressbar"
              aria-label="Repository analysis progress"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={downloadPercent}
              aria-valuetext={downloadPercent === undefined ? "In progress" : `${Math.round(downloadPercent)}%`}
            >
              <div
                ref={indeterminateProgress}
                className={`h-full rounded-full bg-accent ${downloadPercent === undefined ? "absolute left-0 top-0 w-1/3" : "transition-[width] duration-300 ease-out"}`}
                style={downloadPercent === undefined ? undefined : { width: `${downloadPercent}%` }}
              />
            </div>
          </div>
        )}
        {stage === "choosing" && repository && (
          <section className="mt-9 border-t border-line pt-7" aria-labelledby="module-heading">
            <h2 className="text-lg font-semibold" id="module-heading">Choose a Go module</h2>
            <p className="mt-2 text-sm text-muted">
              {repository.owner}/{repository.repo} contains {repository.modules.length} modules.
            </p>
            <div className="mt-5 divide-y divide-line border-y border-line">
              {repository.modules.map((module) => (
                <button
                  className="flex min-h-13 w-full cursor-pointer items-center justify-between gap-4 px-2 py-3 text-left hover:bg-accent-light"
                  key={module}
                  onClick={() => void begin(repository, module, requestVersion.current, inspectionID)}
                >
                  <span className="min-w-0 truncate font-mono text-sm" title={module}>
                    {moduleLabel(module)}
                  </span>
                  <span className="shrink-0 text-xs text-accent">Explore</span>
                </button>
              ))}
            </div>
          </section>
        )}
      </div>
    </main>
  );
}
