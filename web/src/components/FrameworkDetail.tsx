import { useEffect, useState } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { ArrowLeft, ExternalLink } from "lucide-react";
import { FRAMEWORK_EMOJI } from "../lib/types";
import type { Framework } from "../lib/types";
import { getFrameworkDetail } from "../lib/api";
import { useData } from "../lib/DataContext";

function statusDotClass(alive: boolean, providerStatus?: string): string {
  if (!alive) return "bg-red";
  if (providerStatus === "error") return "bg-yellow";
  return "bg-green";
}

export default function FrameworkDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { agents } = useData();
  const [framework, setFramework] = useState<Framework | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!id) {
      setError("No framework ID specified");
      return;
    }
    getFrameworkDetail(id)
      .then(setFramework)
      .catch((err) => setError(err.message));
  }, [id]);

  const fwAgents = agents.filter((a) => a.framework === id);
  const emoji = FRAMEWORK_EMOJI[id || ""] || "";

  if (error) {
    return (
      <div className="space-y-4">
        <Link to="/install" className="text-xs text-text-muted hover:text-text">&lt; back</Link>
        <p className="text-xs text-red">{error}</p>
      </div>
    );
  }

  if (!framework) {
    return <p className="py-20 text-center text-xs text-text-muted">loading framework...</p>;
  }

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/frameworks/{id}</div>

      <div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(-1)}
            className="rounded p-1 text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> {framework.name} {emoji}
          </h1>
          {framework.installed && (
            <span className="rounded bg-green/10 px-1.5 py-0.5 text-[10px] font-medium text-green">
              installed
            </span>
          )}
        </div>
        <p className="mt-1 text-xs text-text-muted">
          {framework.description}
        </p>
      </div>

      {/* Info grid */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <InfoItem label="language" value={framework.language} />
        <InfoItem label="install method" value={framework.install_method} />
        <InfoItem label="config format" value={framework.config_format} />
        <InfoItem label="binary" value={framework.binary_path} mono />
        <InfoItem label="config path" value={framework.config_path} mono />
        <InfoItem label="default port" value={framework.default_port ? `:${framework.default_port}` : "-"} />
        <InfoItem label="adapter" value={framework.adapter_type} />
        <InfoItem label="log directory" value={framework.log_dir} mono />
        <InfoItem label="log format" value={framework.log_format} />
      </div>

      {/* Links */}
      {(framework.repository || framework.website) && (
        <div className="flex gap-3">
          {framework.repository && (
            <a
              href={framework.repository}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors"
            >
              <ExternalLink className="h-3 w-3" /> repository
            </a>
          )}
          {framework.website && (
            <a
              href={framework.website}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors"
            >
              <ExternalLink className="h-3 w-3" /> website
            </a>
          )}
        </div>
      )}

      {/* Requirements */}
      {framework.requirements?.length > 0 && (
        <div>
          <h3 className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
            requirements
          </h3>
          <div className="flex flex-wrap gap-1.5">
            {framework.requirements.map((req) => (
              <span key={req} className="rounded border border-border bg-surface px-2 py-0.5 text-[10px] text-text-secondary">
                {req}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Agents on this framework */}
      <div>
        <h3 className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
          agents ({fwAgents.length})
        </h3>
        {fwAgents.length === 0 ? (
          <div className="rounded border border-dashed border-border px-4 py-6 text-center text-xs text-text-muted">
            no agents running on this framework
          </div>
        ) : (
          <div className="overflow-hidden rounded border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border bg-surface text-left text-text-muted">
                  <th className="px-4 py-2 font-medium">name</th>
                  <th className="px-4 py-2 font-medium">status</th>
                  <th className="px-4 py-2 font-medium">port</th>
                  <th className="px-4 py-2 font-medium">provider</th>
                  <th className="px-4 py-2 font-medium">model</th>
                </tr>
              </thead>
              <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
                {fwAgents.map((agent) => (
                  <tr
                    key={agent.name}
                    onClick={() => navigate(`/agents/${agent.name}/chat`)}
                    className="group cursor-pointer transition-colors hover:bg-surface-hover/50"
                  >
                    <td className="px-4 py-2 transition-colors group-hover:text-accent">
                      <span className="flex items-center gap-2">
                        <span className={`h-1.5 w-1.5 rounded-full ${statusDotClass(agent.alive, agent.status?.provider_status)}`} />
                        {agent.display_name || agent.name}
                      </span>
                    </td>
                    <td className="px-4 py-2">
                      <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                        agent.alive ? "bg-green/10 text-green" : "bg-red/10 text-red"
                      }`}>
                        {agent.alive ? "running" : "stopped"}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-text-secondary">:{agent.port}</td>
                    <td className="px-4 py-2 text-text-secondary">{agent.status?.provider || "-"}</td>
                    <td className="px-4 py-2 text-text-secondary truncate max-w-48">{agent.status?.model || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="flex gap-2">
        <Link
          to="/install"
          className="rounded border border-border px-3 py-1.5 text-xs text-text-secondary hover:text-text hover:border-text-muted/50 transition-colors"
        >
          {framework.installed ? "reinstall / update" : "install"}
        </Link>
      </div>
    </div>
  );
}

function InfoItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded border border-border bg-surface p-3">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">{label}</p>
      <p className={`mt-1 text-xs text-text truncate ${mono ? "font-mono" : ""}`} title={value}>
        {value || "-"}
      </p>
    </div>
  );
}
