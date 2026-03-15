export interface AgentInfo {
  name: string;
  framework: string;
  host: string;
  port: number;
  alive: boolean;
  health?: HealthStatus;
  status?: AgentStatus;
}

export interface HealthStatus {
  alive: boolean;
  uptime: number;
  ram_bytes: number;
  pid: number;
  components?: Record<string, ComponentHealth>;
}

export interface ComponentHealth {
  status: string;
  last_error?: string;
  restart_count: number;
}

export interface AgentStatus {
  provider: string;
  model: string;
  channels: string[];
  skills: number;
  errors_24h: number;
  gateway_port: number;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
}

export interface ActivityEvent {
  timestamp: string;
  type: string;
  summary: string;
  full_content?: string;
  fields?: Record<string, unknown>;
}

export interface Session {
  key: string;
  title: string;
  last_message?: string;
  channel?: string;
}

export interface SessionsResponse {
  supported: boolean;
  sessions: Session[];
}

export interface ChatMessage {
  timestamp: string;
  role: "user" | "assistant";
  content: string;
  channel?: string;
}
