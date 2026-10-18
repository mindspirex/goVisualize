"use client";

import { type CSSProperties, type ReactNode } from "react";
import useResizable from "@/components/use-resizable";

const minimum = 280;

function panelMaximum(element: HTMLDivElement) {
  return element.clientWidth - 288;
}

export default function ResizablePanels({
  children,
}: {
  children: [ReactNode, ReactNode];
}) {
  const { container, size: width, maximum, dragging, handlers } = useResizable({
    axis: "x",
    initial: 560,
    minimum,
    initialMaximum: 800,
    maximumFor: panelMaximum,
  });

  return (
    <div
      ref={container}
      style={{ "--panel-width": `${width}px` } as CSSProperties}
      className={`grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_8px_var(--panel-width)] ${dragging ? "cursor-col-resize select-none" : ""}`}
    >
      {children[0]}
      <div
        role="separator"
        aria-label="Resize code panel"
        aria-controls="code-panel"
        aria-orientation="vertical"
        aria-valuemin={minimum}
        aria-valuemax={maximum}
        aria-valuenow={Math.round(width)}
        aria-valuetext={`${Math.round(width)} pixels`}
        tabIndex={0}
        title="Drag to resize. Use arrow keys when focused. Double-click to reset."
        className={`group z-10 flex touch-none cursor-col-resize items-center justify-center bg-surface hover:bg-accent-light focus-visible:bg-accent-light focus-visible:outline-none ${dragging ? "bg-accent-light" : ""}`}
        {...handlers}
      >
        <span
          aria-hidden="true"
          className="h-10 w-0.5 rounded-full bg-line group-hover:bg-accent group-focus-visible:bg-accent"
        />
      </div>
      {children[1]}
    </div>
  );
}
