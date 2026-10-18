"use client";

import dynamic from "next/dynamic";
import { useSyncExternalStore } from "react";

const GitHubExplorer = dynamic(() => import("@/components/github-explorer"));
const mobileQuery = "(max-width: 650px), (hover: none) and (pointer: coarse)";

function subscribe(onChange: () => void) {
  const query = window.matchMedia(mobileQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

function desktopSnapshot() {
  return !window.matchMedia(mobileQuery).matches;
}

function serverSnapshot() {
  return null;
}

export default function DesktopApp() {
  const desktop = useSyncExternalStore<boolean | null>(
    subscribe,
    desktopSnapshot,
    serverSnapshot,
  );

  if (desktop === null) return null;
  if (desktop) return <GitHubExplorer />;

  return (
    <main className="flex min-h-dvh items-center justify-center px-6 py-7 text-center">
      <h1 className="text-lg font-medium">please open on pc</h1>
    </main>
  );
}
