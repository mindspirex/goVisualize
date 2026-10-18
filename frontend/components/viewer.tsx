"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Controls,
  Handle,
  Position,
  ReactFlow,
  type Edge,
  type NodeProps,
  type Node,
  type ReactFlowInstance,
} from "@xyflow/react";
import Prism from "prismjs";
import ResizablePanels from "@/components/resizable-panels";
import "prismjs/components/prism-go";
import {
  layoutTree,
  shortName,
  type FunctionInfo,
  type Creation,
  type Occurrence,
  type Tree,
  type TreeEdge,
} from "@/lib/tree";

const buttonClasses =
  "min-h-9 cursor-pointer rounded-md border border-line bg-surface px-3 py-[7px] text-ink transition-colors hover:border-[#8cafa0] hover:bg-accent-light disabled:cursor-default disabled:opacity-50";
const tabClasses =
  "min-h-9 cursor-pointer border-b-2 px-0 py-2.5 text-xs disabled:cursor-default disabled:opacity-50";

function creationLabel(type: string) {
  const name = type
    .replace(/(?:[A-Za-z_][\w.-]*\/)*[A-Za-z_][\w.-]*\./g, "")
    .replace(/^\*+/, "");
  return `${name}{}`;
}

type TreeNode = Node<
  {
    fn: FunctionInfo;
    creation?: Creation;
    occurrence: Occurrence;
    count: number;
    expanded: boolean;
    toggle: () => void;
  },
  "tree"
>;
function TreeCard({ data, selected }: NodeProps<TreeNode>) {
  const label = data.creation
    ? creationLabel(data.creation.type)
    : `${shortName(data.fn.id)}()`;
  return (
    <div
      className={`h-full w-full rounded-xl text-ink ${selected ? "border-2 border-accent bg-selected px-[13px] py-[11px]" : "border border-node-border bg-surface px-3.5 py-3"} ${data.occurrence.recursive ? "border-dashed" : ""}`}
    >
      <Handle type="target" position={Position.Top} />
      {data.occurrence.recursive && (
        <div className="mb-[7px] text-[11px] text-muted">
          Recursion reference
        </div>
      )}
      <strong
        className="block truncate font-mono text-[15px] font-semibold"
        title={data.creation?.type ?? data.fn.id}
      >
        {label}
      </strong>
      <div className="mt-2 flex h-7 items-center justify-between gap-2">
        <span className="truncate text-[11px] text-muted" title={data.fn.file}>
          {data.fn.file}
        </span>
        {data.count > 0 && !data.occurrence.recursive && (
          <button
            className="nodrag min-h-7 cursor-pointer rounded-md border border-line bg-surface px-2 py-[3px] text-xs whitespace-nowrap hover:bg-accent-light"
            aria-label={`${data.expanded ? "Collapse" : "Expand"} ${shortName(data.fn.id)}`}
            aria-expanded={data.expanded}
            onClick={(event) => {
              event.stopPropagation();
              data.toggle();
            }}
          >
            {data.expanded ? "−" : "+"} {data.count}
          </button>
        )}
      </div>
      {data.count > 0 && !data.occurrence.recursive && (
        <Handle type="source" position={Position.Bottom} />
      )}
    </div>
  );
}
const nodeTypes = { tree: TreeCard };

function packagePath(id: string) {
  const name = id.split("@")[0];
  const separator = name.indexOf(".", name.lastIndexOf("/") + 1);
  return separator < 0 ? name : name.slice(0, separator);
}

function Code({ source, line }: { source: FunctionInfo; line?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const html = useMemo(
    () => Prism.highlight(source.code, Prism.languages.go, "go").split("\n"),
    [source.code],
  );
  useEffect(() => {
    const panel = ref.current;
    const highlighted = panel?.querySelector<HTMLElement>(".highlight-line");
    if (!panel || !highlighted) return;
    const panelBounds = panel.getBoundingClientRect();
    const lineBounds = highlighted.getBoundingClientRect();
    panel.scrollTop +=
      lineBounds.top -
      panelBounds.top -
      panel.clientHeight / 2 +
      lineBounds.height / 2;
  }, [source.id, line]);
  return (
    <div
      className="min-h-0 flex-1 overflow-auto bg-code py-[15px]"
      ref={ref}
      tabIndex={0}
      aria-label={`Source code for ${shortName(source.id)}`}
    >
      <pre className="m-0 min-w-max font-mono text-xs leading-[1.9] [tab-size:4]">
        {html.map((content, index) => (
          <div
            className={`flex min-w-full pr-6 ${index + 1 === line ? "highlight-line bg-highlight" : ""}`}
            key={index}
          >
            <span className="w-12 shrink-0 pr-[17px] text-right text-code-muted select-none">
              {index + 1}
            </span>
            <code dangerouslySetInnerHTML={{ __html: content || " " }} />
          </div>
        ))}
      </pre>
    </div>
  );
}

function buildNodes(
  occurrences: Occurrence[],
  functions: Map<string, FunctionInfo>,
  creations: Map<string, Creation>,
  counts: Map<string, number>,
  expanded: Set<string>,
  selected: string,
  toggle: (key: string) => void,
): TreeNode[] {
  return occurrences.map((item) => {
    const creation = creations.get(item.targetID);
    const count = creation ? 0 : counts.get(item.targetID) ?? 0;
    const height = item.recursive ? 109 : 85;
    return {
      id: item.key,
      type: "tree",
      position: { x: item.x, y: item.y },
      width: 220,
      height,
      handles: [
        {
          type: "target",
          position: Position.Top,
          x: 107,
          y: -3,
          width: 6,
          height: 6,
        },
        ...(count > 0 && !item.recursive
          ? [{
              type: "source" as const,
              position: Position.Bottom,
              x: 107,
              y: height - 3,
              width: 6,
              height: 6,
            }]
          : []),
      ],
      selected: item.key === selected,
      data: {
        fn: functions.get(item.targetID) ?? functions.get(item.edge!.caller)!,
        creation,
        occurrence: item,
        count,
        expanded: expanded.has(item.key),
        toggle: () => toggle(item.key),
      },
    };
  });
}

function buildEdges(
  occurrences: Occurrence[],
  creations: Map<string, Creation>,
  selected: string,
): Edge[] {
  return occurrences
    .filter((item) => item.parent)
    .map((item) => ({
      id: item.key,
      source: item.parent!,
      target: item.key,
      type: "smoothstep",
      selectable: true,
      focusable: true,
      ariaLabel: `${creations.has(item.targetID) ? `Create ${creations.get(item.targetID)!.type}` : `Call to ${shortName(item.targetID)}`} at line ${item.edge!.line}`,
      style: {
        stroke: item.key === selected ? "#167365" : "#9bafa9",
        strokeWidth: item.key === selected ? 2 : 1.5,
      },
    }));
}

function TreeCanvas({
  tree,
  root,
  nodes,
  edges,
  truncated,
  onRootChange,
  onReset,
  onInit,
  onSelectNode,
  onSelectEdge,
}: {
  tree: Tree;
  root: string;
  nodes: TreeNode[];
  edges: Edge[];
  truncated: boolean;
  onRootChange: (root: string) => void;
  onReset: () => void;
  onInit: (instance: ReactFlowInstance<TreeNode>) => void;
  onSelectNode: (id: string) => void;
  onSelectEdge: (id: string) => void;
}) {
  const canvas = useRef<HTMLElement>(null);
  const flow = useRef<ReactFlowInstance<TreeNode> | null>(null);
  useEffect(() => {
    if (!canvas.current) return;
    let timer: ReturnType<typeof setTimeout>;
    const observer = new ResizeObserver(() => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        void flow.current?.fitView({ padding: 0.22, duration: 250, maxZoom: 1 });
      }, 80);
    });
    observer.observe(canvas.current);
    return () => {
      clearTimeout(timer);
      observer.disconnect();
    };
  }, []);
  const roots = tree.entrypoints;
  const showSelector = roots.length > 1;
  return (
    <section
      ref={canvas}
      className="relative min-h-90 overflow-hidden"
      aria-label="Function call tree"
    >
      <div className="pointer-events-none absolute inset-x-[22px] top-4 z-5 flex items-center justify-end gap-3 text-xs text-muted">
        {showSelector && (
          <label className="pointer-events-auto min-w-0 flex-1">
            <span className="sr-only">Entrypoint</span>
            <select
              className="min-h-9 w-full max-w-80 rounded-md border border-line bg-surface px-3 py-[7px] text-xs text-ellipsis text-ink"
              value={root}
              onChange={(event) => onRootChange(event.target.value)}
            >
              {roots.map((id) => (
                <option key={id} value={id}>
                  {id}
                </option>
              ))}
            </select>
          </label>
        )}
        <button
          className={`${buttonClasses} pointer-events-auto shrink-0 text-xs`}
          onClick={onReset}
        >
          Reset branches
        </button>
      </div>
      <ReactFlow<TreeNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onInit={(instance) => {
          flow.current = instance;
          onInit(instance);
        }}
        onNodesChange={(changes) => {
          const change = changes.find(
            (item) => item.type === "select" && item.selected,
          );
          if (change?.type === "select") onSelectNode(change.id);
        }}
        onEdgesChange={(changes) => {
          const change = changes.find(
            (item) => item.type === "select" && item.selected,
          );
          if (change?.type === "select") onSelectEdge(change.id);
        }}
        onNodeClick={(_, node) => onSelectNode(node.id)}
        onEdgeClick={(_, edge) => onSelectEdge(edge.target)}
        nodesDraggable={false}
        nodesConnectable={false}
        fitView
        fitViewOptions={{ maxZoom: 1, padding: 0.22 }}
        minZoom={0.1}
        maxZoom={1.8}
      >
        <Controls
          showInteractive={false}
          showFitView={false}
          orientation="horizontal"
        />
      </ReactFlow>
      {truncated && (
        <div
          className="pointer-events-none absolute right-5 bottom-5 left-[70px] text-right text-[11px] text-muted"
          role="status"
        >
          Showing 400 nodes. Collapse branches to explore more.
        </div>
      )}
    </section>
  );
}

function SourceInspector({
  current,
  creation,
  definition,
  source,
  site,
  callSite,
  onCallSiteChange,
}: {
  current?: Occurrence;
  creation?: Creation;
  definition?: FunctionInfo;
  source?: FunctionInfo;
  site?: TreeEdge;
  callSite: boolean;
  onCallSiteChange: (value: boolean) => void;
}) {
  return (
    <aside
      id="code-panel"
      className="flex min-h-0 min-w-0 flex-col overflow-hidden border-l border-line bg-surface pt-[22px]"
      aria-label="Source details"
    >
      {source && (
        <>
          <h2
            className="mx-6 mb-2 text-lg font-semibold tracking-tight [overflow-wrap:anywhere]"
            title={creation?.type ?? source.id}
          >
            {creation ? creationLabel(creation.type) : shortName(source.id)}
          </h2>
          <dl className="mx-6 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
            <dt className="text-muted">Package</dt>
            <dd className="font-mono [overflow-wrap:anywhere]">
              {packagePath(source.id)}
            </dd>
            <dt className="text-muted">File</dt>
            <dd className="font-mono [overflow-wrap:anywhere]">
              {source.file}
            </dd>
          </dl>
          {current?.recursive && (
            <p className="mx-6 my-4 bg-accent-light p-3 text-xs leading-relaxed">
              Recursive call. This branch stops here.
            </p>
          )}
          {creation && !definition ? (
            <div className="mx-6 mt-6 border-b border-line py-2.5 text-xs text-accent">
              Creation site · Line {current?.edge?.line}
            </div>
          ) : (
            <div className="mx-6 mt-[25px] flex gap-[18px] border-b border-line">
              <button
                className={`${tabClasses} ${!callSite ? "border-accent font-semibold text-accent" : "border-transparent hover:text-accent"}`}
                onClick={() => onCallSiteChange(false)}
              >
                {creation ? "Definition" : "Function"}
              </button>
              <button
                disabled={!site}
                className={`${tabClasses} ${callSite ? "border-accent font-semibold text-accent" : "border-transparent hover:text-accent"}`}
                onClick={() => onCallSiteChange(true)}
              >
                Call site
              </button>
            </div>
          )}
          <Code
            source={source}
            line={callSite || (creation && !definition) ? site?.line : undefined}
          />
        </>
      )}
    </aside>
  );
}

function EmptyState({ tree }: { tree: Tree | null }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-[18px] p-10 text-center">
      <h2 className="text-[28px] font-semibold tracking-tight">
        {tree ? "No main function found in this module." : "Graph unavailable"}
      </h2>
      {!tree && (
        <p className="leading-relaxed text-muted">
          Try loading the repository again.
        </p>
      )}
    </div>
  );
}

export default function Viewer({ treeUrl }: { treeUrl: string }) {
  const [tree, setTree] = useState<Tree | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [root, setRoot] = useState("");
  const [expanded, setExpanded] = useState(new Set(["root"]));
  const [placement, setPlacement] = useState<{
    tree: Tree;
    root: string;
    nodes: Occurrence[];
    anchor: string;
  } | null>(null);
  const [selected, setSelected] = useState("root");
  const [callSite, setCallSite] = useState(false);
  const [focusRequest, setFocusRequest] = useState(0);
  const flow = useRef<ReactFlowInstance<TreeNode> | null>(null);
  const accept = useCallback((next: Tree) => {
    setPlacement(null);
    setTree(next);
    setRoot(next.entrypoints[0] || "");
    setExpanded(new Set(["root"]));
    setSelected("root");
    setCallSite(false);
    setError("");
  }, []);
  const reload = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(treeUrl, { cache: "no-store" });
      const value = await response.json();
      if (!response.ok) throw new Error(value.error);
      accept(value as Tree);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Could not load the graph.",
      );
    } finally {
      setLoading(false);
    }
  }, [accept, treeUrl]);
  useEffect(() => {
    void reload();
  }, [reload]);
  const functions = useMemo(
    () => new Map(tree?.functions.map((fn) => [fn.id, fn]) ?? []),
    [tree],
  );
  const creations = useMemo(
    () => new Map(tree?.creations.map((item) => [item.id, item]) ?? []),
    [tree],
  );
  const structs = useMemo(
    () => new Map(tree?.structs.map((item) => [item.id, item]) ?? []),
    [tree],
  );
  const childCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const edge of tree?.edges ?? []) {
      counts.set(edge.caller, (counts.get(edge.caller) ?? 0) + 1);
    }
    return counts;
  }, [tree]);
  const layout = useMemo(
    () =>
      tree && root
        ? layoutTree(
            tree,
            root,
            expanded,
            400,
            placement?.tree === tree && placement.root === root
              ? placement.nodes
              : [],
            placement?.anchor,
          )
        : { nodes: [], truncated: false },
    [tree, root, expanded, placement],
  );
  const toggle = useCallback(
    (key: string) => {
      if (tree) setPlacement({ tree, root, nodes: layout.nodes, anchor: key });
      if (expanded.has(key) && selected.startsWith(key + "/")) {
        setSelected(key);
        setCallSite(false);
      }
      setExpanded((previous) => {
        const next = new Set(previous);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        return next;
      });
    },
    [tree, root, layout.nodes, expanded, selected],
  );
  const nodes = buildNodes(
    layout.nodes,
    functions,
    creations,
    childCounts,
    expanded,
    selected,
    toggle,
  );
  const edges = buildEdges(layout.nodes, creations, selected);
  const current =
    layout.nodes.find((item) => item.key === selected) ?? layout.nodes[0];
  const creation = current && creations.get(current.targetID);
  const definition = creation?.definition
    ? structs.get(creation.definition)
    : undefined;
  const site = current?.edge;
  const source =
    definition && !callSite
      ? definition
      : current &&
        functions.get(
          (callSite || creation) && site ? site.caller : current.targetID,
        );
  function resetBranches() {
    setPlacement(null);
    setExpanded(new Set(["root"]));
    setSelected("root");
    setCallSite(false);
    setFocusRequest((previous) => previous + 1);
  }
  function selectNode(id: string) {
    setSelected(id);
    setCallSite(false);
  }
  function selectEdge(id: string) {
    setSelected(id);
    setCallSite(Boolean(layout.nodes.find((item) => item.key === id)?.edge));
  }
  useEffect(() => {
    const timer = setTimeout(() => {
      void flow.current?.fitView({ padding: 0.22, duration: 250, maxZoom: 1 });
    }, 80);
    return () => clearTimeout(timer);
  }, [root, focusRequest]);
  return (
    <main className="flex min-h-0 flex-1 flex-col">
      <h1 className="sr-only">Function tree</h1>
      {error && (
        <div
          className="mx-7 mb-4 flex items-center justify-between gap-[15px] rounded-md bg-[#fff1e9] px-4 py-3 text-[13px] text-[#804224]"
          role="alert"
        >
          {error}{" "}
          <button className={buttonClasses} onClick={() => void reload()}>
            Try again
          </button>
        </div>
      )}
      {loading && !tree ? (
        <div
          className="flex flex-1 flex-col items-center justify-center gap-[18px] p-10 text-center"
          role="status"
        >
          <h2 className="text-[28px] font-semibold tracking-tight">
            Loading your tree…
          </h2>
        </div>
      ) : !tree || !tree.entrypoints.length || !tree.functions.length ? (
        <EmptyState tree={tree} />
      ) : (
        <ResizablePanels>
          <TreeCanvas
            tree={tree}
            root={root}
            nodes={nodes}
            edges={edges}
            truncated={layout.truncated}
            onRootChange={(value) => {
              resetBranches();
              setRoot(value);
            }}
            onReset={resetBranches}
            onInit={(instance) => {
              flow.current = instance;
            }}
            onSelectNode={selectNode}
            onSelectEdge={selectEdge}
          />
          <SourceInspector
            current={current}
            creation={creation}
            definition={definition}
            source={source}
            site={site}
            callSite={callSite}
            onCallSiteChange={setCallSite}
          />
        </ResizablePanels>
      )}
    </main>
  );
}
