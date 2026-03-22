import { useEffect, useState, useCallback } from "react";
import { RefreshCw, Users, Sparkles } from "lucide-react";
import type { Persona, PersonaCategory } from "../lib/types";
import {
  fetchPersonas,
  fetchPersonaCategories,
  installPersona,
} from "../lib/api";
import PersonaCard from "./PersonaCard";

export default function PersonasPage() {
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [categories, setCategories] = useState<PersonaCategory[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeCategory, setActiveCategory] = useState<string | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const [p, c] = await Promise.all([
        fetchPersonas(),
        fetchPersonaCategories(),
      ]);
      setPersonas(p);
      setCategories(c);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load personas");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleInstall = async (personaId: string) => {
    try {
      setInstalling(personaId);
      await installPersona(personaId);
      // Refresh to show updated status
      const updated = await fetchPersonas();
      setPersonas(updated);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Failed to install persona",
      );
    } finally {
      setInstalling(null);
    }
  };

  const filtered = activeCategory
    ? personas.filter((p) => p.category === activeCategory)
    : personas;

  const installedCount = personas.filter((p) => p.installed).length;

  return (
    <div className="space-y-6">
      <div className="text-xs text-text-muted">~/personas</div>

      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> grow_your_team
          </h1>
          <p className="mt-1 text-xs text-text-muted">
            // add personas to shape how your agents think and act
          </p>
        </div>
        <button
          onClick={load}
          disabled={loading}
          className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
        >
          <RefreshCw
            className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`}
          />
          $ refresh
        </button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-3 gap-4">
        <div className="rounded border border-border bg-surface p-4">
          <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            available
          </p>
          <p className="mt-1.5 text-xl font-bold text-text">
            {personas.length}
          </p>
        </div>
        <div className="rounded border border-border bg-surface p-4">
          <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            installed
          </p>
          <p className="mt-1.5 text-xl font-bold text-accent">
            {installedCount}
          </p>
        </div>
        <div className="rounded border border-border bg-surface p-4">
          <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            categories
          </p>
          <p className="mt-1.5 text-xl font-bold text-text">
            {categories.length}
          </p>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 text-xs text-red">
          {error}
        </div>
      )}

      {/* Category filter */}
      <div className="flex items-center gap-2">
        <button
          onClick={() => setActiveCategory(null)}
          className={`px-3 py-1.5 rounded text-xs font-medium transition-colors ${
            activeCategory === null
              ? "bg-accent text-white"
              : "text-text-secondary hover:text-text hover:bg-surface-hover"
          }`}
        >
          all
        </button>
        {categories.map((cat) => (
          <button
            key={cat.id}
            onClick={() =>
              setActiveCategory(activeCategory === cat.id ? null : cat.id)
            }
            className={`px-3 py-1.5 rounded text-xs font-medium transition-colors ${
              activeCategory === cat.id
                ? "bg-accent text-white"
                : "text-text-secondary hover:text-text hover:bg-surface-hover"
            }`}
          >
            {cat.name}
          </button>
        ))}
      </div>

      {/* Loading */}
      {loading && personas.length === 0 && (
        <div className="py-12 text-center text-xs text-text-muted">
          <Sparkles className="w-6 h-6 mx-auto mb-2 animate-pulse text-accent" />
          discovering personas...
        </div>
      )}

      {/* Empty state */}
      {!loading && personas.length === 0 && (
        <div className="rounded border border-border bg-surface p-8 text-center">
          <Users className="w-8 h-8 mx-auto mb-3 text-text-muted" />
          <p className="text-xs text-text-muted">
            no personas available. check the persona catalog configuration.
          </p>
        </div>
      )}

      {/* Persona grid */}
      {filtered.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((persona) => (
            <PersonaCard
              key={persona.id}
              persona={persona}
              onInstall={handleInstall}
              installingId={installing ?? undefined}
            />
          ))}
        </div>
      )}

      {/* Filtered empty */}
      {!loading && personas.length > 0 && filtered.length === 0 && (
        <div className="py-8 text-center text-xs text-text-muted">
          no personas in this category
        </div>
      )}
    </div>
  );
}
