import { useCallback, useState, useEffect } from "react";

const STORAGE_KEY = "eyrie-agent-metrics";
const MAX_SAMPLES = 50;

export interface AgentMetrics {
  latencies: number[];     // milliseconds, most recent last
  inputTokens: number[];   // per-message input token counts
  outputTokens: number[];  // per-message output token counts
  totalCost: number;       // cumulative cost in USD
  lastUpdated: string;     // ISO timestamp
}

type MetricsStore = Record<string, AgentMetrics>;

function emptyMetrics(): AgentMetrics {
  return { latencies: [], inputTokens: [], outputTokens: [], totalCost: 0, lastUpdated: "" };
}

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
  } catch {}
}

function ensureMetrics(store: MetricsStore, name: string): AgentMetrics {
  if (!store[name]) store[name] = emptyMetrics();
  const m = store[name];
  // Migrate old format that only had latencies
  if (!m.inputTokens) m.inputTokens = [];
  if (!m.outputTokens) m.outputTokens = [];
  if (!m.totalCost) m.totalCost = 0;
  return m;
}

/** Record a latency measurement (ms) for an agent. */
export function recordLatency(agentName: string, latencyMs: number) {
  const store = readStore();
  const m = ensureMetrics(store, agentName);
  m.latencies.push(Math.round(latencyMs));
  if (m.latencies.length > MAX_SAMPLES) m.latencies = m.latencies.slice(-MAX_SAMPLES);
  m.lastUpdated = new Date().toISOString();
  writeStore(store);
}

/** Record token usage and cost for an agent. */
export function recordUsage(agentName: string, inputTokens: number, outputTokens: number, costUsd: number) {
  const store = readStore();
  const m = ensureMetrics(store, agentName);
  if (inputTokens > 0) {
    m.inputTokens.push(inputTokens);
    if (m.inputTokens.length > MAX_SAMPLES) m.inputTokens = m.inputTokens.slice(-MAX_SAMPLES);
  }
  if (outputTokens > 0) {
    m.outputTokens.push(outputTokens);
    if (m.outputTokens.length > MAX_SAMPLES) m.outputTokens = m.outputTokens.slice(-MAX_SAMPLES);
  }
  if (costUsd > 0) m.totalCost += costUsd;
  m.lastUpdated = new Date().toISOString();
  writeStore(store);
}

/** Get p50/p90/p99 latencies for an agent. */
export function latencyPercentiles(agentName: string): { p50: number; p90: number; p99: number } | null {
  const store = readStore();
  const m = store[agentName];
  if (!m || !m.latencies?.length) return null;
  const sorted = [...m.latencies].sort((a, b) => a - b);
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
