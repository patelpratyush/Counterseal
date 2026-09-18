"use client";
import { useEffect, useMemo, useRef } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  MarkerType,
  type ReactFlowInstance,
  type NodeProps,
  type Node,
  type Edge,
} from "@xyflow/react";
import { Bot, ShieldX, ArrowDownRight } from "lucide-react";
import type { Chain } from "@/lib/types";
import "@xyflow/react/dist/style.css";
type AgentData = {
  label: string;
  idLabel: string;
  depth: number;
  actions: number;
  denied: boolean;
  revoked: boolean;
};
type AgentNode = Node<AgentData, "agent">;
function Agent({ data, selected }: NodeProps<AgentNode>) {
  return (
    <div
      className={`agent-node ${selected ? "is-selected" : ""} ${data.denied ? "is-denied" : ""}`}
    >
      <Handle type="target" position={Position.Left} />
      <div className="node-top">
        <span className="node-icon">
          {data.denied ? <ShieldX size={19} /> : <Bot size={19} />}
        </span>
        <span>
          {data.denied
            ? "BLOCKED PROPOSAL"
            : data.revoked
              ? "REVOKED ENVELOPE"
              : `AGENT · DEPTH ${data.depth}`}
        </span>
      </div>
      <strong>{data.label}</strong>
      <code>{data.idLabel}</code>
      <div className="node-bottom">
        <span>
          {data.denied
            ? "No authority issued"
            : `${data.actions} permitted actions`}
        </span>
        <ArrowDownRight size={14} />
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
const nodeTypes = { agent: Agent };
export default function RunGraph({
  chain,
  selected,
  onSelect,
}: {
  chain: Chain;
  selected: string;
  onSelect: (id: string) => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const flow = useRef<ReactFlowInstance<AgentNode, Edge> | null>(null);
  useEffect(() => {
    const element = container.current;
    if (!element) return;
    let frame = 0;
    let width = 0;
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width === width) return;
      width = entry.contentRect.width;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        void flow.current?.fitView({ padding: 0.22, maxZoom: 1, duration: 0 });
      });
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, []);
  const { nodes, edges } = useMemo(() => {
    const rows = new Map<number, number>();
    const nodes: AgentNode[] = chain.envelopes.map(
      ({ envelope: e, revoked_at }) => {
        const depth = e.delegation.current_depth;
        const row = rows.get(depth) || 0;
        rows.set(depth, row + 1);
        return {
          id: e.id,
          type: "agent",
          position: { x: depth * 330, y: row * 200 },
          selected: selected === e.id,
          data: {
            label: e.recipient.agent,
            idLabel: e.id,
            depth,
            actions: e.allowed_actions.length,
            denied: false,
            revoked: !!revoked_at,
          },
        };
      },
    );
    const edges: Edge[] = chain.handoffs.map((h) => {
      let target = h.child_envelope_id;
      if (!target) {
        target = `denied:${h.id}`;
        const parent = chain.envelopes.find(
          (v) => v.envelope.id === h.parent_envelope_id,
        )?.envelope;
        const depth = (parent?.delegation.current_depth || 0) + 1;
        const row = rows.get(depth) || 0;
        rows.set(depth, row + 1);
        nodes.push({
          id: target,
          type: "agent",
          position: { x: depth * 330, y: row * 200 },
          selected: selected === h.id,
          data: {
            label: "Delegation denied",
            idLabel: h.proposed_child_id,
            depth,
            actions: 0,
            denied: true,
            revoked: false,
          },
        });
      }
      const denied = h.result.decision === "DENY";
      const color = denied ? "var(--danger)" : "var(--green)";
      return {
        id: h.id,
        source: h.parent_envelope_id,
        target,
        type: "smoothstep",
        label: h.result.decision,
        selected: selected === h.id,
        style: {
          stroke: color,
          strokeWidth: selected === h.id ? 3 : 1.5,
          strokeDasharray: denied ? "5 4" : undefined,
        },
        labelStyle: { fill: color, fontSize: 10, fontWeight: 700 },
        labelBgStyle: { fill: "var(--surface)" },
        markerEnd: { type: MarkerType.ArrowClosed, color },
      };
    });
    return { nodes, edges };
  }, [chain, selected]);
  return (
    <div ref={container} className="graph-canvas" aria-label="Delegation graph">
      <ReactFlow
        onInit={(instance) => {
          flow.current = instance;
        }}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.22, maxZoom: 1 }}
        minZoom={0.1}
        maxZoom={1.5}
        nodesDraggable={false}
        nodesConnectable={false}
        onNodeClick={(_, n) =>
          onSelect(n.id.startsWith("denied:") ? n.id.slice(7) : n.id)
        }
        onEdgeClick={(_, e) => onSelect(e.id)}
      >
        <Background gap={22} size={1} color="var(--dot-grid)" />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
