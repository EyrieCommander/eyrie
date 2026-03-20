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
  cpu_percent: number;
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
  readonly?: boolean;
}

export interface SessionsResponse {
  supported: boolean;
  sessions: Session[];
}

export interface ChatPart {
  type: "text" | "tool_call";
  text?: string;
  id?: string;
  name?: string;
  args?: Record<string, unknown>;
  output?: string;
  error?: boolean;
}

export interface ChatMessage {
  timestamp: string;
  role: "user" | "assistant";
  content: string;
  channel?: string;
  parts?: ChatPart[];
}

export interface ChatEvent {
  type: "delta" | "tool_start" | "tool_result" | "done" | "error";
  content?: string;
  tool?: string;
  tool_id?: string;
  args?: Record<string, unknown>;
  output?: string;
  success?: boolean;
  error?: string;
}

export interface ConfigField {
  key: string;
  label: string;
  type: "text" | "number" | "select" | "checkbox" | "multiselect";
  default?: unknown;
  required: boolean;
  description: string;
  options?: string[];
  min?: number;
  max?: number;
}

export interface ConfigSchema {
  common_fields: ConfigField[];
  api_key_hint: string;
}

export interface Framework {
  id: string;
  name: string;
  description: string;
  language: string;
  repository: string;
  website?: string;
  install_method: string;
  install_cmd: string;
  requirements: string[];
  config_format: string;
  config_path: string;
  config_dir: string;
  binary_path: string;
  adapter_type: string;
  default_port?: number;
  start_cmd: string;
  stop_cmd: string;
  status_cmd: string;
  restart_cmd?: string;
  pid_file?: string;
  state_file?: string;
  health_url?: string;
  log_dir: string;
  log_format: string;
  config_schema?: ConfigSchema;
  installed?: boolean; // Added by backend
}

export interface InstallProgress {
  framework_id: string;
  phase: string;
  status: "running" | "success" | "error";
  progress: number;
  message: string;
  error?: string;
  started_at: string;
  completed_at?: string;
}

export interface InstallLogEvent {
  type: "log";
  message: string;
}
