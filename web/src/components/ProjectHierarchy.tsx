import { Bot, Crown, Shield } from "lucide-react";

interface HierarchyNode {
  name: string;
  role: "commander" | "captain" | "talon";
  status: "running" | "busy" | "stopped" | "starting" | "provisioning" | "error" | "unknown";
  framework?: string;
  port?: number;
  onClick?: () => void;
}

interface ProjectHierarchyProps {
  commander?: HierarchyNode | null;
  captain?: HierarchyNode | null;
  talons: HierarchyNode[];
}

function statusDotClass(status: HierarchyNode["status"]): string {
  switch (status) {
    case "starting":
    case "provisioning":
      return "bg-yellow-400 animate-pulse";
    case "running":
      return "bg-green";
    case "busy":
      return "bg-yellow-400";
    case "error":
      return "bg-red";
    case "stopped":
      return "bg-text-muted";
    default:
      return "bg-text-muted";
  }
}

const ROLE_ICON = {
  commander: Crown,
  captain: Shield,
  talon: Bot,
} as const;

function HierarchyNodeCard({ node }: { node: HierarchyNode }) {
  const Icon = ROLE_ICON[node.role];
  return (
    <button
      onClick={node.onClick}
      className="group flex items-center gap-2 rounded border border-border bg-surface px-3 py-2 text-xs transition-all hover:border-accent/40 hover:bg-surface-hover"
    >
      <span className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${statusDotClass(node.status)}`} />
      <Icon className="h-3 w-3 text-text-muted group-hover:text-accent" />
      <span className="font-medium text-text">{node.name}</span>
      <span className="text-[10px] text-text-muted">{node.role}</span>
    </button>
  );
}

function Connector({ vertical = false }: { vertical?: boolean }) {
  if (vertical) {
    return <div className="mx-auto h-4 w-px bg-border" />;
  }
  return <div className="mx-1 h-px w-4 bg-border" />;
}

export function ProjectHierarchy({ commander, captain, talons }: ProjectHierarchyProps) {
  if (!commander && !captain && talons.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col items-center gap-0 rounded border border-border bg-bg/50 px-4 py-3">
      {/* Commander */}
      {commander && (
        <>
          <HierarchyNodeCard node={commander} />
          {(captain || talons.length > 0) && <Connector vertical />}
        </>
      )}

      {/* Captain */}
      {captain && (
        <>
          <HierarchyNodeCard node={captain} />
          <Connector vertical />
        </>
      )}

      {/* Talons */}
      <div className="flex flex-col items-center gap-0">
        {talons.length > 0 ? (
          <>
            {/* Horizontal branch line */}
            {talons.length > 1 && (
              <div className="flex items-center">
                {talons.map((_, i) => (
                  <div key={i} className="flex items-center">
                    {i > 0 && <div className="h-px w-6 bg-border" />}
                    <div className="h-3 w-px bg-border" />
                  </div>
                ))}
              </div>
            )}
            <div className="flex flex-wrap items-start justify-center gap-2">
              {talons.map((t, i) => (
                <HierarchyNodeCard key={`${t.name}-${i}`} node={t} />
              ))}
            </div>
          </>
        ) : (captain || commander) && (
          /* Empty talon placeholder */
          <div className="flex items-center gap-2 rounded border border-dashed border-border px-3 py-2 text-[10px] text-text-muted">
            <Bot className="h-3 w-3" />
            <span>no talons yet</span>
          </div>
        )}
      </div>
    </div>
  );
}
