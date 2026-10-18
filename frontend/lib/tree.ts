export type FunctionInfo = {
  id: string;
  description: string;
  file: string;
  code: string;
};
export type StructDefinition = FunctionInfo;
export type Creation = { id: string; type: string; definition?: string };
export type TreeEdge = { caller: string; target: string; line: number };
export type Tree = {
  entrypoints: string[];
  functions: FunctionInfo[];
  structs: StructDefinition[];
  creations: Creation[];
  edges: TreeEdge[];
};
export type Occurrence = {
  key: string;
  targetID: string;
  parent?: string;
  edge?: TreeEdge;
  recursive: boolean;
  x: number;
  y: number;
};

export function layoutTree(
  tree: Tree,
  root: string,
  expanded: Set<string>,
  limit = 400,
  previous: Occurrence[] = [],
  anchor?: string,
) {
  const byCaller = new Map<string, TreeEdge[]>();
  for (const edge of tree.edges) {
    if (!byCaller.has(edge.caller)) byCaller.set(edge.caller, []);
    byCaller.get(edge.caller)!.push(edge);
  }
  const nodes: Occurrence[] = [];
  let leaf = 0;
  let truncated = false;
  function visit(
    id: string,
    key: string,
    depth: number,
    ancestors: Set<string>,
    parent?: string,
    edge?: TreeEdge,
  ): Occurrence | undefined {
    if (nodes.length >= limit) {
      truncated = true;
      return;
    }
    const recursive = ancestors.has(id);
    const node: Occurrence = {
      key,
      targetID: id,
      parent,
      edge,
      recursive,
      x: 0,
      y: depth * 160,
    };
    nodes.push(node);
    const children: Occurrence[] = [];
    if (!recursive && expanded.has(key)) {
      const path = new Set(ancestors).add(id);
      for (const [index, child] of (byCaller.get(id) ?? []).entries()) {
        const result = visit(
          child.target,
          `${key}/${index}`,
          depth + 1,
          path,
          key,
          child,
        );
        if (result) children.push(result);
      }
    }
    node.x = children.length
      ? (children[0].x + children[children.length - 1].x) / 2
      : leaf++ * 260;
    return node;
  }
  visit(root, "root", 0, new Set());
  if (previous.length && anchor) stabilize(nodes, previous, anchor);
  return { nodes, truncated };
}

export function shortName(id: string) {
  const tail = id.slice(id.lastIndexOf("/") + 1);
  return tail.slice(tail.indexOf(".") + 1);
}

function stabilize(
  nodes: Occurrence[],
  previous: Occurrence[],
  anchor: string,
) {
  const before = previous.find((node) => node.key === anchor);
  const after = nodes.find((node) => node.key === anchor);
  if (!before || !after) return;
  const offset = before.x - after.x;
  for (const node of nodes) node.x += offset;
}
