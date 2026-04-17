export interface AgentInfo {
  name: string;
  display_name?: string;
  framework: string;
  host: string;
  port: number;
  alive: boolean;
  health?: HealthStatus;
  status?: AgentStatus;
  commander_capable: boolean;
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
  provider_status?: string; // "ok", "error", or undefined (unknown)
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
  pending?: boolean;
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
  code?: string; // Machine-readable error code (e.g. "auth_expired", "agent_unreachable")
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
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
  installed?: boolean;   // binary exists on disk
  configured?: boolean;  // config file exists (onboarding complete)
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

export interface Persona {
  id: string;
  name: string;
  role: string;
  description: string;
  icon: string;
  category: string;
  preferred_model: string;
  temperature?: number;
  max_tokens?: number;
  reasoning_level?: string;
  system_prompt: string;
  tools: string[];
  traits: string[];
  preferred_framework?: string;
  installed?: boolean;
  agent_name?: string;
  agent_alive?: boolean;
}

export interface PersonaCategory {
  id: string;
  name: string;
  description: string;
}

// --- Instance types ---

export type HierarchyRole = "commander" | "captain" | "talon" | "";

export interface AgentInstance {
  id: string;
  name: string;
  display_name: string;
  framework: string;
  persona_id?: string;
  hierarchy_role?: HierarchyRole;
  project_id?: string;
  parent_id?: string;
  port: number;
  config_path: string;
  workspace_path: string;
  status: string;
  created_at: string;
  created_by: string;
}

export interface CreateInstanceRequest {
  name: string;
  framework: string;
  persona_id?: string;
  hierarchy_role?: HierarchyRole;
  project_id?: string;
  parent_id?: string;
  model?: string;
  auto_start?: boolean;
  created_by?: string;
}

// --- Project types ---

export interface Project {
  id: string;
  name: string;
  description: string;
  goal?: string;
  orchestrator_id?: string;
  role_agent_ids?: string[];
  status: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  session_key?: string;
}

export interface CreateProjectRequest {
  name: string;
  description: string;
  goal?: string;
}

// --- Hierarchy types ---

export interface CommanderInfo {
  name: string;
  display_name: string;
  status: string;
  hierarchy_role: string;
}

export interface HierarchyTree {
  commander?: CommanderInfo;
  projects: ProjectTree[];
}

export interface ProjectTree {
  project: Project;
  captain?: AgentInstance;
  talons: AgentInstance[];
}

export interface ProjectChatMessage {
  id: string;
  sender: string;
  role: string; // "user", "commander", "captain", "talon"
  content: string;
  timestamp: string;
  mention?: string;
  parts?: ChatPart[];
  detail?: string; // expandable content (e.g., full briefing text)
}

// --- Key vault types ---

export interface KeyEntry {
  provider: string;
  masked_key: string;
  has_key: boolean;
}

export interface SetKeyResponse {
  provider: string;
  masked_key: string;
  valid: boolean;
  verified: boolean;
}

export interface ValidateKeyResponse {
  valid: boolean;
  error?: string;
}

export const FRAMEWORK_EMOJI: Record<string, string> = {
  zeroclaw: "🌀",
  openclaw: "🦞",
  hermes: "🔱",
  picoclaw: "🎯",
  embedded: "⚡",
};
