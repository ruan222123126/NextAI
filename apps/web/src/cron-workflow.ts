import { t } from "./i18n.js";

export type CronWorkflowNodeType = "start" | "text_event" | "delay" | "if_event";

export interface CronWorkflowNode {
  id: string;
  type: CronWorkflowNodeType;
  title?: string;
  x: number;
  y: number;
  text?: string;
  delay_seconds?: number;
  if_condition?: string;
  continue_on_error?: boolean;
}

export interface CronWorkflowEdge {
  id: string;
  source: string;
  target: string;
}

export interface CronWorkflowViewport {
  pan_x?: number;
  pan_y?: number;
  zoom?: number;
}

export interface CronWorkflowSpec {
  version: "v1";
  viewport?: CronWorkflowViewport;
  nodes: CronWorkflowNode[];
  edges: CronWorkflowEdge[];
}

interface CronWorkflowCanvasOptions {
  viewport: HTMLElement;
  canvas: HTMLElement;
  edgesLayer: SVGSVGElement;
  nodesLayer: HTMLElement;
  nodeEditor: HTMLElement;
  zoomLabel: HTMLElement;
  onChange?: (workflow: CronWorkflowSpec) => void;
  onStatus?: (message: string, tone: "info" | "error") => void;
}

const NODE_WIDTH = 220;
const NODE_HEIGHT = 112;
const CANVAS_WIDTH = 3200;
const CANVAS_HEIGHT = 2000;
const ZOOM_MIN = 0.5;
const ZOOM_MAX = 1.8;
const IF_CONDITION_PATTERN = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*(?:"([^"]*)"|'([^']*)'|(\S+))\s*$/;
const IF_ALLOWED_FIELDS = new Set(["job_id", "job_name", "channel", "user_id", "session_id", "task_type"]);
const DEFAULT_NODE_TITLE_ALIASES: Record<CronWorkflowNodeType, Set<string>> = {
  start: new Set(["start"]),
  text_event: new Set(["text event", "text_event"]),
  delay: new Set(["delay"]),
  if_event: new Set(["if event", "if_event"]),
};

function getNodeTypeLabel(type: CronWorkflowNodeType): string {
  if (type === "start") {
    return t("cron.nodeTypeStart");
  }
  if (type === "text_event") {
    return t("cron.nodeTypeTextEvent");
  }
  if (type === "delay") {
    return t("cron.nodeTypeDelay");
  }
  if (type === "if_event") {
    return t("cron.nodeTypeIfEvent");
  }
  return type;
}

function resolveNodeDisplayTitle(node: CronWorkflowNode): string {
  const fallback = node.id;
  const raw = (node.title ?? "").trim();
  if (raw === "") {
    return fallback;
  }
  const normalized = raw.toLowerCase();
  if (DEFAULT_NODE_TITLE_ALIASES[node.type]?.has(normalized)) {
    return getNodeTypeLabel(node.type);
  }
  return raw;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function validateIfCondition(raw: string): string | null {
  const text = raw.trim();
  if (text === "") {
    return "if_condition is required";
  }
  const match = IF_CONDITION_PATTERN.exec(text);
  if (!match) {
    return "if_condition must match `<field> == <value>` or `<field> != <value>`";
  }
  const field = (match[1] ?? "").toLowerCase();
  if (!IF_ALLOWED_FIELDS.has(field)) {
    return `if_condition field ${field} is unsupported`;
  }
  return null;
}

function normalizeNode(node: CronWorkflowNode): CronWorkflowNode {
  return {
    id: node.id.trim(),
    type: node.type,
    title: (node.title ?? "").trim(),
    x: Number.isFinite(node.x) ? node.x : 0,
    y: Number.isFinite(node.y) ? node.y : 0,
    text: (node.text ?? "").trim(),
    delay_seconds: Number.isFinite(node.delay_seconds) ? Number(node.delay_seconds) : 0,
    if_condition: (node.if_condition ?? "").trim(),
    continue_on_error: Boolean(node.continue_on_error),
  };
}

function normalizeEdge(edge: CronWorkflowEdge): CronWorkflowEdge {
  return {
    id: edge.id.trim(),
    source: edge.source.trim(),
    target: edge.target.trim(),
  };
}

function normalizeViewport(viewport?: CronWorkflowViewport): CronWorkflowViewport {
  const panX = Number.isFinite(viewport?.pan_x) ? Number(viewport?.pan_x) : 0;
  const panY = Number.isFinite(viewport?.pan_y) ? Number(viewport?.pan_y) : 0;
  const zoom = clamp(Number.isFinite(viewport?.zoom) ? Number(viewport?.zoom) : 1, ZOOM_MIN, ZOOM_MAX);
  return {
    pan_x: panX,
    pan_y: panY,
    zoom,
  };
}

function mapFromEdges(edges: CronWorkflowEdge[]): Map<string, string> {
  const nextByID = new Map<string, string>();
  for (const edge of edges) {
    nextByID.set(edge.source, edge.target);
  }
  return nextByID;
}

function hasCycle(nodes: CronWorkflowNode[], edges: CronWorkflowEdge[]): boolean {
  const nextByID = mapFromEdges(edges);
  for (const node of nodes) {
    const seen = new Set<string>([node.id]);
    let cursor = node.id;
    for (let i = 0; i < nodes.length + 2; i += 1) {
      const next = nextByID.get(cursor);
      if (!next) {
        break;
      }
      if (seen.has(next)) {
        return true;
      }
      seen.add(next);
      cursor = next;
    }
  }
  return false;
}

export function createDefaultCronWorkflow(): CronWorkflowSpec {
  return {
    version: "v1",
    viewport: {
      pan_x: 0,
      pan_y: 0,
      zoom: 1,
    },
    nodes: [
      {
        id: "start",
        type: "start",
        title: getNodeTypeLabel("start"),
        x: 80,
        y: 80,
      },
      {
        id: "node-1",
        type: "text_event",
        title: getNodeTypeLabel("text_event"),
        text: "",
        x: 360,
        y: 80,
        continue_on_error: false,
      },
    ],
    edges: [
      {
        id: "edge-start-node-1",
        source: "start",
        target: "node-1",
      },
    ],
  };
}

export function validateCronWorkflowSpec(spec: CronWorkflowSpec): string | null {
  if (!spec || spec.version !== "v1") {
    return "workflow.version must be v1";
  }
  if (!Array.isArray(spec.nodes) || spec.nodes.length < 2) {
    return "workflow.nodes requires at least 2 nodes";
  }
  if (!Array.isArray(spec.edges) || spec.edges.length < 1) {
    return "workflow.edges requires at least 1 edge";
  }

  const nodeByID = new Map<string, CronWorkflowNode>();
  let startCount = 0;
  for (const rawNode of spec.nodes) {
    const node = normalizeNode(rawNode);
    if (!node.id) {
      return "workflow node id is required";
    }
    if (nodeByID.has(node.id)) {
      return `workflow node id duplicated: ${node.id}`;
    }
    if (node.type === "start") {
      startCount += 1;
    } else if (node.type === "text_event") {
      if (!node.text) {
        return `workflow node ${node.id} requires non-empty text`;
      }
    } else if (node.type === "delay") {
      if ((node.delay_seconds ?? 0) < 0) {
        return `workflow node ${node.id} delay_seconds must be >= 0`;
      }
    } else if (node.type === "if_event") {
      const issue = validateIfCondition(node.if_condition ?? "");
      if (issue) {
        return `workflow node ${node.id} ${issue}`;
      }
    } else {
      return `workflow node ${node.id} has unsupported type`;
    }
    nodeByID.set(node.id, node);
  }

  if (startCount !== 1) {
    return "workflow requires exactly one start node";
  }

  const edgeIDs = new Set<string>();
  const inDegree = new Map<string, number>();
  const outDegree = new Map<string, number>();
  for (const rawEdge of spec.edges) {
    const edge = normalizeEdge(rawEdge);
    if (!edge.id) {
      return "workflow edge id is required";
    }
    if (edgeIDs.has(edge.id)) {
      return `workflow edge id duplicated: ${edge.id}`;
    }
    edgeIDs.add(edge.id);
    if (!edge.source || !edge.target) {
      return `workflow edge ${edge.id} requires source and target`;
    }
    if (!nodeByID.has(edge.source) || !nodeByID.has(edge.target)) {
      return `workflow edge ${edge.id} references unknown node`;
    }
    outDegree.set(edge.source, (outDegree.get(edge.source) ?? 0) + 1);
    inDegree.set(edge.target, (inDegree.get(edge.target) ?? 0) + 1);
    if ((outDegree.get(edge.source) ?? 0) > 1) {
      return `workflow node ${edge.source} has more than one outgoing edge`;
    }
    if ((inDegree.get(edge.target) ?? 0) > 1) {
      return `workflow node ${edge.target} has more than one incoming edge`;
    }
  }

  const startNode = spec.nodes.map((node) => normalizeNode(node)).find((node) => node.type === "start");
  if (!startNode) {
    return "workflow requires start node";
  }
  if ((inDegree.get(startNode.id) ?? 0) > 0) {
    return "workflow start node cannot have incoming edge";
  }
  if ((outDegree.get(startNode.id) ?? 0) === 0) {
    return "workflow start node must connect to executable nodes";
  }
  if (hasCycle(spec.nodes, spec.edges)) {
    return "workflow graph must be acyclic";
  }

  const nextByID = mapFromEdges(spec.edges);
  const reachable = new Set<string>([startNode.id]);
  let cursor = startNode.id;
  for (let i = 0; i < spec.nodes.length + 2; i += 1) {
    const next = nextByID.get(cursor);
    if (!next) {
      break;
    }
    reachable.add(next);
    cursor = next;
  }

  let executableCount = 0;
  for (const node of spec.nodes.map((item) => normalizeNode(item))) {
    if (node.type === "start") {
      continue;
    }
    executableCount += 1;
    if (!reachable.has(node.id)) {
      return `workflow node ${node.id} is not reachable from start`;
    }
  }
  if (executableCount < 1) {
    return "workflow requires at least one executable node";
  }
  return null;
}

export class CronWorkflowCanvas {
  private readonly viewport: HTMLElement;

  private readonly canvas: HTMLElement;

  private readonly edgesLayer: SVGSVGElement;

  private readonly nodesLayer: HTMLElement;

  private readonly nodeEditor: HTMLElement;

  private readonly zoomLabel: HTMLElement;

  private readonly onChange?: (workflow: CronWorkflowSpec) => void;

  private readonly onStatus?: (message: string, tone: "info" | "error") => void;

  private nodes: CronWorkflowNode[] = [];

  private edges: CronWorkflowEdge[] = [];

  private viewportState: CronWorkflowViewport = { pan_x: 0, pan_y: 0, zoom: 1 };

  private selectedNodeID = "";

  private selectedEdgeID = "";

  private linkingSourceNodeID = "";

  private nodeCounter = 1;

  private draggingNodeID = "";

  private panActive = false;

  private pointerStartX = 0;

  private pointerStartY = 0;

  private readonly contextMenu: HTMLDivElement;

  private contextMenuNodeID = "";

  private contextMenuEdgeID = "";

  private contextMenuCanvasX = 360;

  private contextMenuCanvasY = 80;

  private editorNodeID = "";

  constructor(options: CronWorkflowCanvasOptions) {
    this.viewport = options.viewport;
    this.canvas = options.canvas;
    this.edgesLayer = options.edgesLayer;
    this.nodesLayer = options.nodesLayer;
    this.nodeEditor = options.nodeEditor;
    this.zoomLabel = options.zoomLabel;
    this.onChange = options.onChange;
    this.onStatus = options.onStatus;

    this.contextMenu = document.createElement("div");
    this.contextMenu.className = "cron-node-context-menu is-hidden";
    this.contextMenu.setAttribute("role", "menu");

    const addTextButton = document.createElement("button");
    addTextButton.type = "button";
    addTextButton.dataset.cronNodeMenuAction = "add-text";
    addTextButton.textContent = t("cron.addTextNode");
    addTextButton.setAttribute("role", "menuitem");

    const addIfButton = document.createElement("button");
    addIfButton.type = "button";
    addIfButton.dataset.cronNodeMenuAction = "add-if";
    addIfButton.textContent = t("cron.addIfNode");
    addIfButton.setAttribute("role", "menuitem");

    const addDelayButton = document.createElement("button");
    addDelayButton.type = "button";
    addDelayButton.dataset.cronNodeMenuAction = "add-delay";
    addDelayButton.textContent = t("cron.addDelayNode");
    addDelayButton.setAttribute("role", "menuitem");

    const editButton = document.createElement("button");
    editButton.type = "button";
    editButton.dataset.cronNodeMenuAction = "edit";
    editButton.textContent = t("cron.edit");
    editButton.setAttribute("role", "menuitem");

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.dataset.cronNodeMenuAction = "delete";
    deleteButton.textContent = t("cron.delete");
    deleteButton.setAttribute("role", "menuitem");

    this.contextMenu.append(addTextButton, addIfButton, addDelayButton, editButton, deleteButton);
    this.viewport.appendChild(this.contextMenu);

    if (this.nodeEditor.parentElement !== this.viewport) {
      this.viewport.appendChild(this.nodeEditor);
    }
    this.nodeEditor.classList.add("is-hidden");
    this.nodeEditor.setAttribute("aria-hidden", "true");

    this.bindEvents();
    this.setWorkflow(createDefaultCronWorkflow());
  }

  setWorkflow(workflow: CronWorkflowSpec): void {
    const fallback = createDefaultCronWorkflow();
    const source = workflow && Array.isArray(workflow.nodes) && workflow.nodes.length > 0 ? workflow : fallback;
    this.nodes = source.nodes.map((item) => normalizeNode(item));
    this.edges = source.edges.map((item) => normalizeEdge(item));
    this.viewportState = normalizeViewport(source.viewport);
    this.nodeCounter = this.nextNodeCounter();
    if (!this.nodes.some((item) => item.id === this.selectedNodeID)) {
      this.selectedNodeID = this.nodes.find((item) => item.type !== "start")?.id ?? "";
    }
    if (!this.edges.some((item) => item.id === this.selectedEdgeID)) {
      this.selectedEdgeID = "";
    }
    this.linkingSourceNodeID = "";
    this.hideContextMenu();
    this.hideNodeEditor();
    this.render();
    this.emitChange();
  }

  refreshLabels(): void {
    const addTextButton = this.contextMenu.querySelector<HTMLButtonElement>(
      "button[data-cron-node-menu-action=\"add-text\"]",
    );
    if (addTextButton) {
      addTextButton.textContent = t("cron.addTextNode");
    }
    const addIfButton = this.contextMenu.querySelector<HTMLButtonElement>(
      "button[data-cron-node-menu-action=\"add-if\"]",
    );
    if (addIfButton) {
      addIfButton.textContent = t("cron.addIfNode");
    }
    const addDelayButton = this.contextMenu.querySelector<HTMLButtonElement>(
      "button[data-cron-node-menu-action=\"add-delay\"]",
    );
    if (addDelayButton) {
      addDelayButton.textContent = t("cron.addDelayNode");
    }
    const editButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"edit\"]");
    if (editButton) {
      editButton.textContent = t("cron.edit");
    }
    const deleteButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"delete\"]");
    if (deleteButton) {
      deleteButton.textContent = t("cron.delete");
    }
    if (this.editorNodeID !== "") {
      const node = this.nodes.find((item) => item.id === this.editorNodeID);
      if (node) {
        this.renderNodeEditor(node);
      }
    }
    this.renderNodes();
  }

  resetToDefault(): void {
    this.setWorkflow(createDefaultCronWorkflow());
    this.notify(t("cron.resetWorkflowStatus"), "info");
  }

  addTextNode(position?: { x: number; y: number }): void {
    this.addExecutableNode("text_event", position);
  }

  addDelayNode(position?: { x: number; y: number }): void {
    this.addExecutableNode("delay", position);
  }

  addIfNode(position?: { x: number; y: number }): void {
    this.addExecutableNode("if_event", position);
  }

  private addExecutableNode(type: "text_event" | "delay" | "if_event", position?: { x: number; y: number }): void {
    const id = `node-${this.nodeCounter}`;
    this.nodeCounter += 1;
    const fallbackY = 80 + (this.nodes.length - 1) * 140;
    const nextPosition = position
      ? this.clampNodePosition(position.x, position.y)
      : this.clampNodePosition(360, fallbackY);
    const nextNode: CronWorkflowNode = {
      id,
      type,
      title: getNodeTypeLabel(type),
      x: nextPosition.x,
      y: nextPosition.y,
      continue_on_error: false,
    };
    if (type === "text_event") {
      nextNode.text = "";
    } else if (type === "delay") {
      nextNode.delay_seconds = 1;
    } else {
      nextNode.if_condition = "channel == console";
    }
    this.nodes.push(nextNode);
    this.selectedNodeID = id;
    this.selectedEdgeID = "";
    this.render();
    this.emitChange();
  }

  getWorkflow(): CronWorkflowSpec {
    return {
      version: "v1",
      viewport: normalizeViewport(this.viewportState),
      nodes: this.nodes.map((item) => normalizeNode(item)),
      edges: this.edges.map((item) => normalizeEdge(item)),
    };
  }

  validateForSave(): string | null {
    return validateCronWorkflowSpec(this.getWorkflow());
  }

  private bindEvents(): void {
    this.contextMenu.addEventListener("click", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const button = target.closest<HTMLButtonElement>("button[data-cron-node-menu-action]");
      if (!button) {
        return;
      }
      const action = button.dataset.cronNodeMenuAction ?? "";
      const nodeID = this.contextMenuNodeID;
      const edgeID = this.contextMenuEdgeID;
      const contextPosition = {
        x: this.contextMenuCanvasX,
        y: this.contextMenuCanvasY,
      };
      this.hideContextMenu();
      if (action === "add-text") {
        this.addTextNode(contextPosition);
        return;
      }
      if (action === "add-if") {
        this.addIfNode(contextPosition);
        return;
      }
      if (action === "add-delay") {
        this.addDelayNode(contextPosition);
        return;
      }
      if (action === "edit" && nodeID !== "") {
        this.openNodeEditor(nodeID);
        return;
      }
      if (action === "delete") {
        if (edgeID !== "") {
          this.removeEdge(edgeID);
          return;
        }
        if (nodeID !== "") {
          this.removeNode(nodeID);
        }
      }
    });

    this.nodesLayer.addEventListener("click", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      this.hideContextMenu();
      const outHandle = target.closest<HTMLElement>("[data-cron-node-out]");
      if (outHandle) {
        this.hideNodeEditor();
        this.selectedEdgeID = "";
        this.linkingSourceNodeID = outHandle.dataset.cronNodeOut ?? "";
        this.render();
        return;
      }
      const inHandle = target.closest<HTMLElement>("[data-cron-node-in]");
      if (inHandle) {
        this.hideNodeEditor();
        const targetID = inHandle.dataset.cronNodeIn ?? "";
        if (this.linkingSourceNodeID !== "" && targetID !== "") {
          this.connectNodes(this.linkingSourceNodeID, targetID);
        }
        this.linkingSourceNodeID = "";
        this.render();
        return;
      }
      const nodeElement = target.closest<HTMLElement>("[data-cron-node-id]");
      if (!nodeElement) {
        if (this.selectedEdgeID !== "") {
          this.selectedEdgeID = "";
          this.renderEdges();
        }
        return;
      }
      const nodeID = nodeElement.dataset.cronNodeId ?? "";
      this.selectedNodeID = nodeID;
      this.selectedEdgeID = "";
      this.hideNodeEditor();
      this.render();
    });

    this.nodesLayer.addEventListener("contextmenu", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      event.preventDefault();
      const nodeElement = target.closest<HTMLElement>("[data-cron-node-id]");
      if (!nodeElement) {
        this.hideNodeEditor();
        const position = this.clientPointToCanvas(event.clientX, event.clientY);
        this.showCanvasContextMenu(event.clientX, event.clientY, position.x, position.y);
        return;
      }
      const nodeID = nodeElement.dataset.cronNodeId ?? "";
      if (nodeID === "") {
        this.hideContextMenu();
        return;
      }
      this.selectedNodeID = nodeID;
      this.selectedEdgeID = "";
      this.hideNodeEditor();
      this.showNodeContextMenu(nodeID, event.clientX, event.clientY);
      this.renderNodes();
      this.renderEdges();
    });

    this.edgesLayer.addEventListener("click", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      this.hideContextMenu();
      const edgePath = target.closest<SVGPathElement>("[data-edge-id]");
      if (!edgePath) {
        if (this.selectedEdgeID !== "") {
          this.selectedEdgeID = "";
          this.renderEdges();
        }
        return;
      }
      const edgeID = edgePath.dataset.edgeId ?? "";
      if (edgeID === "") {
        return;
      }
      this.selectedNodeID = "";
      this.selectedEdgeID = edgeID;
      this.hideNodeEditor();
      this.render();
    });

    this.edgesLayer.addEventListener("contextmenu", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const edgePath = target.closest<SVGPathElement>("[data-edge-id]");
      if (!edgePath) {
        return;
      }
      event.preventDefault();
      const edgeID = edgePath.dataset.edgeId ?? "";
      if (edgeID === "") {
        return;
      }
      this.selectedNodeID = "";
      this.selectedEdgeID = edgeID;
      this.hideNodeEditor();
      this.showEdgeContextMenu(edgeID, event.clientX, event.clientY);
      this.renderNodes();
      this.renderEdges();
    });

    this.nodesLayer.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) {
        return;
      }
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      if (target.closest("[data-cron-node-out], [data-cron-node-in]")) {
        return;
      }
      const nodeElement = target.closest<HTMLElement>("[data-cron-node-id]");
      if (!nodeElement) {
        return;
      }
      const nodeID = nodeElement.dataset.cronNodeId ?? "";
      if (!nodeID) {
        return;
      }
      this.hideContextMenu();
      this.hideNodeEditor();
      this.draggingNodeID = nodeID;
      this.pointerStartX = event.clientX;
      this.pointerStartY = event.clientY;
      this.selectedNodeID = nodeID;
      this.selectedEdgeID = "";
      nodeElement.setPointerCapture(event.pointerId);
    });

    this.nodesLayer.addEventListener("pointermove", (event) => {
      if (this.draggingNodeID === "") {
        return;
      }
      const node = this.nodes.find((item) => item.id === this.draggingNodeID);
      if (!node) {
        return;
      }
      const zoom = this.viewportState.zoom ?? 1;
      node.x += (event.clientX - this.pointerStartX) / zoom;
      node.y += (event.clientY - this.pointerStartY) / zoom;
      this.pointerStartX = event.clientX;
      this.pointerStartY = event.clientY;
      this.render();
      this.emitChange();
    });

    this.nodesLayer.addEventListener("pointerup", () => {
      this.draggingNodeID = "";
    });
    this.nodesLayer.addEventListener("pointercancel", () => {
      this.draggingNodeID = "";
    });

    this.viewport.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) {
        return;
      }
      const target = event.target;
      if (
        target instanceof Element &&
        (target.closest(".cron-workflow-node-editor") || target.closest(".cron-node-context-menu"))
      ) {
        return;
      }
      if (event.target !== this.viewport && !(event.target instanceof SVGSVGElement)) {
        return;
      }
      this.hideContextMenu();
      this.panActive = true;
      this.pointerStartX = event.clientX;
      this.pointerStartY = event.clientY;
      this.viewport.setPointerCapture(event.pointerId);
      this.viewport.classList.add("is-panning");
    });

    this.viewport.addEventListener("pointermove", (event) => {
      if (!this.panActive) {
        return;
      }
      this.viewportState.pan_x = (this.viewportState.pan_x ?? 0) + (event.clientX - this.pointerStartX);
      this.viewportState.pan_y = (this.viewportState.pan_y ?? 0) + (event.clientY - this.pointerStartY);
      this.pointerStartX = event.clientX;
      this.pointerStartY = event.clientY;
      this.renderCanvasTransform();
      this.emitChange();
    });

    this.viewport.addEventListener("pointerup", () => {
      this.panActive = false;
      this.viewport.classList.remove("is-panning");
    });
    this.viewport.addEventListener("pointercancel", () => {
      this.panActive = false;
      this.viewport.classList.remove("is-panning");
    });

    this.viewport.addEventListener(
      "wheel",
      (event) => {
        event.preventDefault();
        const nextZoom = clamp((this.viewportState.zoom ?? 1) * (event.deltaY < 0 ? 1.1 : 0.9), ZOOM_MIN, ZOOM_MAX);
        this.viewportState.zoom = nextZoom;
        this.renderCanvasTransform();
        this.emitChange();
      },
      { passive: false },
    );

    this.nodeEditor.addEventListener("pointerdown", (event) => {
      if (event.target !== this.nodeEditor) {
        return;
      }
      this.hideContextMenu();
      this.hideNodeEditor();
    });

    window.addEventListener("pointerdown", (event) => {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      const inMenu = this.contextMenu.contains(target);
      const inEditor = this.nodeEditor.contains(target);
      const inNode = target instanceof Element && target.closest("[data-cron-node-id]") !== null;
      const inEdge = target instanceof Element && target.closest("[data-edge-id]") !== null;
      if (!inMenu && !inNode) {
        this.hideContextMenu();
      }
      if (!inMenu && !inEditor && !inNode && !inEdge) {
        this.hideNodeEditor();
      }
    });

    window.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        this.hideContextMenu();
        this.hideNodeEditor();
        return;
      }
      if (event.key !== "Delete" && event.key !== "Backspace") {
        return;
      }
      if (this.selectedEdgeID === "") {
        return;
      }
      const activeElement = document.activeElement;
      if (
        activeElement instanceof HTMLInputElement ||
        activeElement instanceof HTMLTextAreaElement ||
        activeElement instanceof HTMLSelectElement ||
        (activeElement instanceof HTMLElement && activeElement.isContentEditable)
      ) {
        return;
      }
      event.preventDefault();
      this.removeEdge(this.selectedEdgeID);
    });

    window.addEventListener("resize", () => {
      this.hideContextMenu();
    });
  }

  private connectNodes(sourceID: string, targetID: string): void {
    if (sourceID === targetID) {
      this.notify(t("cron.nodeErrorConnectSelf"), "error");
      return;
    }
    if (this.edges.some((item) => item.source === sourceID)) {
      this.notify(t("cron.nodeErrorSourceHasOutgoing"), "error");
      return;
    }
    if (this.edges.some((item) => item.target === targetID)) {
      this.notify(t("cron.nodeErrorTargetHasIncoming"), "error");
      return;
    }
    const edge: CronWorkflowEdge = {
      id: `edge-${sourceID}-${targetID}`,
      source: sourceID,
      target: targetID,
    };
    this.edges.push(edge);
    if (hasCycle(this.nodes, this.edges)) {
      this.edges = this.edges.filter((item) => item !== edge);
      this.notify(t("cron.nodeErrorCycle"), "error");
      return;
    }
    this.selectedEdgeID = edge.id;
    this.selectedNodeID = "";
    this.emitChange();
    this.render();
  }

  private nextNodeCounter(): number {
    let max = 0;
    for (const node of this.nodes) {
      const match = /^node-(\d+)$/.exec(node.id);
      if (!match) {
        continue;
      }
      const value = Number.parseInt(match[1] ?? "0", 10);
      if (Number.isFinite(value) && value > max) {
        max = value;
      }
    }
    return max + 1;
  }

  private clampNodePosition(x: number, y: number): { x: number; y: number } {
    return {
      x: clamp(x, 0, CANVAS_WIDTH - NODE_WIDTH),
      y: clamp(y, 0, CANVAS_HEIGHT - NODE_HEIGHT),
    };
  }

  private clientPointToCanvas(clientX: number, clientY: number): { x: number; y: number } {
    const viewportRect = this.viewport.getBoundingClientRect();
    const panX = this.viewportState.pan_x ?? 0;
    const panY = this.viewportState.pan_y ?? 0;
    const zoom = this.viewportState.zoom ?? 1;
    const x = (clientX - viewportRect.left - panX) / zoom - NODE_WIDTH / 2;
    const y = (clientY - viewportRect.top - panY) / zoom - NODE_HEIGHT / 2;
    return this.clampNodePosition(x, y);
  }

  private notify(message: string, tone: "info" | "error"): void {
    if (this.onStatus) {
      this.onStatus(message, tone);
    }
  }

  private emitChange(): void {
    if (this.onChange) {
      this.onChange(this.getWorkflow());
    }
  }

  private render(): void {
    this.renderCanvasTransform();
    this.renderNodes();
    this.renderEdges();
  }

  private renderCanvasTransform(): void {
    const panX = this.viewportState.pan_x ?? 0;
    const panY = this.viewportState.pan_y ?? 0;
    const zoom = this.viewportState.zoom ?? 1;
    this.canvas.style.transform = `translate(${panX}px, ${panY}px) scale(${zoom})`;
    this.zoomLabel.textContent = `${Math.round(zoom * 100)}%`;
  }

  private removeNode(nodeID: string): void {
    const targetNode = this.nodes.find((item) => item.id === nodeID);
    if (!targetNode) {
      return;
    }
    if (targetNode.type === "start") {
      this.notify(t("cron.nodeErrorStartImmutable"), "error");
      return;
    }
    this.nodes = this.nodes.filter((item) => item.id !== nodeID);
    this.edges = this.edges.filter((item) => item.source !== nodeID && item.target !== nodeID);
    if (!this.edges.some((item) => item.id === this.selectedEdgeID)) {
      this.selectedEdgeID = "";
    }
    if (this.selectedNodeID === nodeID) {
      this.selectedNodeID = this.nodes.find((item) => item.type !== "start")?.id ?? "";
    }
    if (this.editorNodeID === nodeID) {
      this.hideNodeEditor();
    }
    this.hideContextMenu();
    this.render();
    this.emitChange();
  }

  private removeEdge(edgeID: string): void {
    const existed = this.edges.some((item) => item.id === edgeID);
    if (!existed) {
      return;
    }
    this.edges = this.edges.filter((item) => item.id !== edgeID);
    this.selectedEdgeID = "";
    this.hideContextMenu();
    this.renderEdges();
    this.emitChange();
  }

  private showNodeContextMenu(nodeID: string, clientX: number, clientY: number): void {
    this.contextMenuNodeID = nodeID;
    this.contextMenuEdgeID = "";
    this.setContextMenuMode("node");
    const node = this.nodes.find((item) => item.id === nodeID);
    const deleteButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"delete\"]");
    if (deleteButton) {
      deleteButton.disabled = node?.type === "start";
    }
    this.positionContextMenu(clientX, clientY);
  }

  private showCanvasContextMenu(clientX: number, clientY: number, nodeX: number, nodeY: number): void {
    this.contextMenuNodeID = "";
    this.contextMenuEdgeID = "";
    this.contextMenuCanvasX = nodeX;
    this.contextMenuCanvasY = nodeY;
    this.setContextMenuMode("canvas");
    this.positionContextMenu(clientX, clientY);
  }

  private showEdgeContextMenu(edgeID: string, clientX: number, clientY: number): void {
    this.contextMenuNodeID = "";
    this.contextMenuEdgeID = edgeID;
    this.setContextMenuMode("edge");
    const deleteButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"delete\"]");
    if (deleteButton) {
      deleteButton.disabled = false;
    }
    this.positionContextMenu(clientX, clientY);
  }

  private setContextMenuMode(mode: "node" | "canvas" | "edge"): void {
    const addTextButton = this.contextMenu.querySelector<HTMLButtonElement>(
      "button[data-cron-node-menu-action=\"add-text\"]",
    );
    if (addTextButton) {
      addTextButton.hidden = mode !== "canvas";
    }
    const addIfButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"add-if\"]");
    if (addIfButton) {
      addIfButton.hidden = mode !== "canvas";
    }
    const addDelayButton = this.contextMenu.querySelector<HTMLButtonElement>(
      "button[data-cron-node-menu-action=\"add-delay\"]",
    );
    if (addDelayButton) {
      addDelayButton.hidden = mode !== "canvas";
    }
    const editButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"edit\"]");
    if (editButton) {
      editButton.hidden = mode !== "node";
    }
    const deleteButton = this.contextMenu.querySelector<HTMLButtonElement>("button[data-cron-node-menu-action=\"delete\"]");
    if (deleteButton) {
      deleteButton.hidden = mode === "canvas";
    }
  }

  private positionContextMenu(clientX: number, clientY: number): void {
    this.contextMenu.classList.remove("is-hidden");
    this.contextMenu.setAttribute("aria-hidden", "false");
    const viewportRect = this.viewport.getBoundingClientRect();
    const menuRect = this.contextMenu.getBoundingClientRect();
    const rawLeft = clientX - viewportRect.left;
    const rawTop = clientY - viewportRect.top;
    const left = clamp(rawLeft, 8, Math.max(8, viewportRect.width - menuRect.width - 8));
    const top = clamp(rawTop, 8, Math.max(8, viewportRect.height - menuRect.height - 8));
    this.contextMenu.style.left = `${left}px`;
    this.contextMenu.style.top = `${top}px`;
  }

  private hideContextMenu(): void {
    this.contextMenuNodeID = "";
    this.contextMenuEdgeID = "";
    this.contextMenu.classList.add("is-hidden");
    this.contextMenu.setAttribute("aria-hidden", "true");
  }

  private openNodeEditor(nodeID: string): void {
    const node = this.nodes.find((item) => item.id === nodeID);
    if (!node) {
      this.hideNodeEditor();
      return;
    }
    this.hideContextMenu();
    this.selectedNodeID = nodeID;
    this.editorNodeID = nodeID;
    this.nodeEditor.classList.remove("is-hidden");
    this.nodeEditor.setAttribute("aria-hidden", "false");
    this.renderNodeEditor(node);
    this.renderNodes();
  }

  private hideNodeEditor(): void {
    this.editorNodeID = "";
    this.nodeEditor.innerHTML = "";
    this.nodeEditor.classList.add("is-hidden");
    this.nodeEditor.setAttribute("aria-hidden", "true");
  }

  private renderNodes(): void {
    this.nodesLayer.innerHTML = "";
    for (const node of this.nodes) {
      const element = document.createElement("article");
      element.className = "cron-node-card";
      if (node.id === this.selectedNodeID) {
        element.classList.add("is-selected");
      }
      element.dataset.cronNodeId = node.id;
      element.style.left = `${node.x}px`;
      element.style.top = `${node.y}px`;

      const head = document.createElement("header");
      head.className = "cron-node-card-head";
      const typeTag = document.createElement("span");
      typeTag.className = "cron-node-type";
      typeTag.textContent = getNodeTypeLabel(node.type);
      const title = document.createElement("strong");
      title.textContent = resolveNodeDisplayTitle(node);
      head.append(typeTag, title);

      const body = document.createElement("div");
      body.className = "cron-node-card-body";
      if (node.type === "text_event") {
        body.textContent = node.text?.trim() ? node.text : t("cron.nodeBodyEmptyText");
      } else if (node.type === "delay") {
        body.textContent = t("cron.nodeBodyDelay", { seconds: Math.max(0, Number(node.delay_seconds ?? 0)) });
      } else if (node.type === "if_event") {
        body.textContent = node.if_condition?.trim()
          ? t("cron.nodeBodyIf", { condition: node.if_condition.trim() })
          : t("cron.nodeBodyEmptyIfCondition");
      } else {
        body.textContent = t("cron.nodeBodyEntryPoint");
      }

      if (node.type !== "start") {
        const inHandle = document.createElement("button");
        inHandle.type = "button";
        inHandle.className = "cron-node-handle cron-node-handle-in";
        inHandle.dataset.cronNodeIn = node.id;
        inHandle.setAttribute("aria-label", t("cron.nodeHandleLinkTo", { nodeId: node.id }));
        inHandle.textContent = "•";
        element.appendChild(inHandle);
      }

      if (node.type === "start" || node.type === "text_event" || node.type === "delay" || node.type === "if_event") {
        const outHandle = document.createElement("button");
        outHandle.type = "button";
        outHandle.className = "cron-node-handle cron-node-handle-out";
        if (this.linkingSourceNodeID === node.id) {
          outHandle.classList.add("is-linking");
        }
        outHandle.dataset.cronNodeOut = node.id;
        outHandle.setAttribute("aria-label", t("cron.nodeHandleLinkFrom", { nodeId: node.id }));
        outHandle.textContent = "•";
        element.appendChild(outHandle);
      }

      element.append(head, body);
      this.nodesLayer.appendChild(element);
    }
  }

  private renderEdges(): void {
    this.edgesLayer.innerHTML = "";
    for (const edge of this.edges) {
      const source = this.nodes.find((item) => item.id === edge.source);
      const target = this.nodes.find((item) => item.id === edge.target);
      if (!source || !target) {
        continue;
      }
      const sourceX = source.x + NODE_WIDTH;
      const sourceY = source.y + NODE_HEIGHT / 2;
      const targetX = target.x;
      const targetY = target.y + NODE_HEIGHT / 2;
      const controlOffset = Math.max(60, Math.abs(targetX - sourceX) * 0.35);
      const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute(
        "d",
        `M ${sourceX} ${sourceY} C ${sourceX + controlOffset} ${sourceY}, ${targetX - controlOffset} ${targetY}, ${targetX} ${targetY}`,
      );
      path.setAttribute("class", "cron-edge-path");
      if (edge.id === this.selectedEdgeID) {
        path.classList.add("is-selected");
      }
      path.setAttribute("data-edge-id", edge.id);
      this.edgesLayer.appendChild(path);
    }
  }

  private renderNodeEditor(node: CronWorkflowNode): void {
    this.nodeEditor.innerHTML = "";

    const panel = document.createElement("section");
    panel.className = "cron-node-editor-modal-panel stack-form";
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    panel.setAttribute("aria-label", `${getNodeTypeLabel(node.type)} (${node.id})`);
    this.nodeEditor.appendChild(panel);

    const head = document.createElement("div");
    head.className = "cron-node-editor-head";

    const title = document.createElement("h4");
    title.textContent = `${getNodeTypeLabel(node.type)} (${node.id})`;

    const closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "cron-node-editor-close";
    closeButton.textContent = t("common.close");
    closeButton.setAttribute("aria-label", t("cron.nodeEditorCloseAria"));
    closeButton.addEventListener("click", () => {
      this.hideNodeEditor();
    });

    head.append(title, closeButton);
    panel.appendChild(head);

    const titleLabel = document.createElement("label");
    const titleSpan = document.createElement("span");
    titleSpan.textContent = t("cron.nodeEditorTitle");
    const titleInput = document.createElement("input");
    titleInput.type = "text";
    titleInput.value = node.title ?? "";
    titleInput.addEventListener("input", () => {
      node.title = titleInput.value;
      this.renderNodes();
      this.emitChange();
    });
    titleLabel.append(titleSpan, titleInput);
    panel.appendChild(titleLabel);

    if (node.type === "text_event") {
      const textLabel = document.createElement("label");
      const textSpan = document.createElement("span");
      textSpan.textContent = t("cron.nodeEditorText");
      const textInput = document.createElement("textarea");
      textInput.rows = 4;
      textInput.value = node.text ?? "";
      textInput.addEventListener("input", () => {
        node.text = textInput.value;
        this.renderNodes();
        this.emitChange();
      });
      textLabel.append(textSpan, textInput);
      panel.appendChild(textLabel);
    }

    if (node.type === "delay") {
      const delayLabel = document.createElement("label");
      const delaySpan = document.createElement("span");
      delaySpan.textContent = t("cron.nodeEditorDelaySeconds");
      const delayInput = document.createElement("input");
      delayInput.type = "number";
      delayInput.min = "0";
      delayInput.value = String(Math.max(0, Number(node.delay_seconds ?? 0)));
      delayInput.addEventListener("input", () => {
        const parsed = Number.parseInt(delayInput.value, 10);
        node.delay_seconds = Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
        this.renderNodes();
        this.emitChange();
      });
      delayLabel.append(delaySpan, delayInput);
      panel.appendChild(delayLabel);
    }

    if (node.type === "if_event") {
      const ifLabel = document.createElement("label");
      const ifSpan = document.createElement("span");
      ifSpan.textContent = t("cron.nodeEditorIfCondition");
      const ifInput = document.createElement("input");
      ifInput.type = "text";
      ifInput.value = node.if_condition ?? "";
      ifInput.placeholder = "channel == console";
      ifInput.addEventListener("input", () => {
        node.if_condition = ifInput.value;
        this.renderNodes();
        this.emitChange();
      });
      ifLabel.append(ifSpan, ifInput);
      panel.appendChild(ifLabel);

      const ifHint = document.createElement("p");
      ifHint.className = "hint";
      ifHint.textContent = t("cron.nodeEditorIfHint");
      panel.appendChild(ifHint);
    }

    if (node.type !== "start") {
      const continueLabel = document.createElement("label");
      continueLabel.className = "checkbox-inline";
      const continueInput = document.createElement("input");
      continueInput.type = "checkbox";
      continueInput.checked = Boolean(node.continue_on_error);
      continueInput.addEventListener("change", () => {
        node.continue_on_error = continueInput.checked;
        this.emitChange();
      });
      const continueSpan = document.createElement("span");
      continueSpan.textContent = t("cron.nodeEditorContinueOnError");
      continueLabel.append(continueInput, continueSpan);
      panel.appendChild(continueLabel);
    }

    const firstField = panel.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea");
    firstField?.focus();
  }
}
