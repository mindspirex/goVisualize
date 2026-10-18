import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from "react";

type Options = {
  axis: "x" | "y";
  initial: number;
  minimum: number;
  initialMaximum: number;
  maximumFor: (element: HTMLDivElement) => number | null;
};

export default function useResizable({
  axis,
  initial,
  minimum,
  initialMaximum,
  maximumFor,
}: Options) {
  const container = useRef<HTMLDivElement>(null);
  const drag = useRef<{ position: number; size: number } | null>(null);
  const [size, setSize] = useState(initial);
  const [maximum, setMaximum] = useState(initialMaximum);
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    const element = container.current;
    if (!element) return;
    const observer = new ResizeObserver(() => {
      const available = maximumFor(element);
      if (available === null) return;
      const max = Math.max(minimum, available);
      setMaximum(max);
      setSize((current) => Math.max(minimum, Math.min(current, max)));
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [maximumFor, minimum]);

  function resize(next: number) {
    setSize(Math.max(minimum, Math.min(maximum, next)));
  }

  function stopDragging() {
    drag.current = null;
    setDragging(false);
  }

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = {
      position: axis === "x" ? event.clientX : event.clientY,
      size,
    };
    setDragging(true);
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    if (!drag.current) return;
    const position = axis === "x" ? event.clientX : event.clientY;
    resize(drag.current.size + drag.current.position - position);
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    stopDragging();
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const step = event.shiftKey ? 50 : 20;
    if (event.key === (axis === "x" ? "ArrowLeft" : "ArrowUp"))
      resize(size + step);
    else if (event.key === (axis === "x" ? "ArrowRight" : "ArrowDown"))
      resize(size - step);
    else if (event.key === "Home") resize(minimum);
    else if (event.key === "End") resize(maximum);
    else return;
    event.preventDefault();
  }

  return {
    container,
    size,
    maximum,
    dragging,
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp,
      onPointerCancel: stopDragging,
      onLostPointerCapture: stopDragging,
      onDoubleClick: () => resize(initial),
      onKeyDown,
    },
  };
}
