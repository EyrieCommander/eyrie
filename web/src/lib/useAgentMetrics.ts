import { useCallback, useState, useEffect } from "react";

const STORAGE_KEY = "eyrie-agent-metrics";
// Keep last 50 latency measurements per agent
const MAX_SAMPLES = 50;

export interface AgentMetrics {
  latencies: number[]; // milliseconds, most recent last
  lastUpdated: string; // ISO timestamp
}

type MetricsStore = Record<string, AgentMetrics>;

function readStore(): MetricsStore {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function writeStore(store: MetricsStore) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch {
    // localStorage full or unavailable
  }
}

/** Record a latency measurement (ms) for an agent. */
export function recordLatency(agentName: string, latencyMs: number) {
  const store = readStore();
  const metrics = store[agentName] || { latencies: [], lastUpdated: "" };
  metrics.latencies.push(Math.round(latencyMs));
  if (metrics.latencies.length > MAX_SAMPLES) {
    metrics.latencies = metrics.latencies.slice(-MAX_SAMPLES);
  }
  metrics.lastUpdated = new Date().toISOString();
  store[agentName] = metrics;
  writeStore(store);
}

/** Get metrics for all agents. */
export function getAllMetrics(): MetricsStore {
  return readStore();
}

/** Get average latency for an agent, or null if no data. */
export function avgLatency(agentName: string): number | null {
  const store = readStore();
  const metrics = store[agentName];
  if (!metrics || metrics.latencies.length === 0) return null;
  const sum = metrics.latencies.reduce((a, b) => a + b, 0);
  return Math.round(sum / metrics.latencies.length);
}

/** Get p50/p90/p99 latencies for an agent. */
export function latencyPercentiles(agentName: string): { p50: number; p90: number; p99: number } | null {
  const store = readStore();
  const metrics = store[agentName];
  if (!metrics || metrics.latencies.length === 0) return null;
  const sorted = [...metrics.latencies].sort((a, b) => a - b);
  const p = (pct: number) => sorted[Math.min(Math.floor(sorted.length * pct), sorted.length - 1)];
  return { p50: p(0.5), p90: p(0.9), p99: p(0.99) };
}

/** Hook that re-reads metrics when they change (polls every 5s). */
export function useAgentMetrics() {
  const [metrics, setMetrics] = useState<MetricsStore>(readStore);

  useEffect(() => {
    const interval = setInterval(() => setMetrics(readStore()), 5000);
    return () => clearInterval(interval);
  }, []);

  const record = useCallback((agentName: string, latencyMs: number) => {
    recordLatency(agentName, latencyMs);
    setMetrics(readStore());
  }, []);

  const reset = useCallback(() => {
    try { localStorage.removeItem(STORAGE_KEY); } catch {}
    setMetrics({});
  }, []);

  return { metrics, record, reset };
}
