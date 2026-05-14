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
  /** Suggested values for text fields — renders as a dropdown with a custom option.
   *  Can be a flat array or a map keyed by another field's value (e.g., provider → models). */
  suggestions?: string[] | Record<string, string[]>;
  /** When suggestions is a map, this is the key of the field whose value selects the list. */
  suggestions_key?: string;
  min?: number;
  max?: number;
  /** Hide behind an "advanced" toggle in the quick setup form. */
  advanced?: boolean;
  /** Layout group name — fields with the same group render side-by-side. */
  group?: string;
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
  min_version?: string;     // minimum compatible version from registry
  latest_version?: string;  // latest known release version from registry
  installed?: boolean;      // binary exists on disk
  configured?: boolean;     // config file exists (onboarding complete)
  version?: string;         // installed binary version (from --version)
  version_status?: "outdated" | "update_available" | "current";
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
  /** Kind of operation this progress represents. Consumed by
   *  frameworkStatus.ts to reliably distinguish install from uninstall
   *  instead of substring-matching `message`. Optional for backward
   *  compatibility; legacy consumers fall back to the message check. */
  operation?: "install" | "uninstall";
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

// --- Local agent mesh types ---

export interface MeshStatus {
  available: boolean;
  root?: string;
  manifest_path?: string;
  updated?: string;
  status?: string;
  project?: string;
  project_id?: string;
  owner?: string;
  parent_agent?: MeshAgentSummary;
  subordinates?: MeshAgentSummary[];
  channels?: MeshChannelSummary;
  inboxes?: MeshInboxSummary[];
  latest_outbox?: MeshNoticeSummary;
  reports?: MeshReportSummary[];
  commander_refs?: MeshCommanderRef[];
  generated_at: string;
  unavailable_text?: string;
}

export interface MeshAgentSummary {
  id: string;
  display_name: string;
  planned_framework: string;
  role: string;
  inbox?: string;
}

export interface MeshChannelSummary {
  broadcasts?: string;
  parent_inbox?: string;
  outbox?: string;
  reports?: string;
  runtime_registry?: string;
  docs_inbox?: string;
  danya_inbox?: string;
  magnus_inbox?: string;
}

export interface MeshInboxSummary {
  recipient: string;
  path: string;
  updated?: string;
  total: number;
  open: number;
  pending_acknowledgements: number;
  notices: MeshNoticeSummary[];
}

export interface MeshNoticeSummary {
  id: string;
  kind?: string;
  title?: string;
  created?: string;
  from?: string;
  to?: string[];
  parent?: string;
  status?: string;
  priority?: string;
  summary?: string;
  request?: string;
  deliverable?: string;
  response?: string;
  source_path?: string;
}

export interface MeshReportSummary {
  path: string;
  title: string;
  modified_at: string;
}

export interface MeshCommanderRef {
  path: string;
  notice?: string;
  source?: string;
}

export interface CommandRoom {
  generated_at: string;
  mesh: MeshStatus;
  board?: CommandRoomBoard;
  runtime_registry: CommandRoomRuntime[];
  development_mesh?: CommandRoomDevelopment;
  zeroclaw_agents: CommandRoomZeroClawAgent[];
  data_sources: CommandRoomDataSource[];
  approval_boundary: string[];
}

export interface CommandRoomBoard {
  path: string;
  generated_at?: string;
  captain?: string;
  domain?: string;
  items: CommandRoomBoardItem[];
}

export interface CommandRoomBoardItem {
  id: string;
  title: string;
  status: string;
  priority: string;
  lane: string;
  owner: string;
  primary_agent: string;
  summary: string;
  next_action: string;
  commander_visible: boolean;
  source?: string;
  linked_item_ref?: string;
}

export interface CommandRoomRuntime {
  id: string;
  display_name: string;
  status: string;
  parent_agent: string;
  owning_domain: string;
  role: string;
  framework: string;
  transport: string;
  workspace?: string;
  current_assignment?: string;
  path: string;
}

export interface CommandRoomDevelopment {
  root: string;
  scope: string;
  status: string;
  provenance: string;
  assignments: CommandRoomDevelopmentNotice[];
  work_items: CommandRoomDevelopmentWorkItem[];
  runtime_smokes: CommandRoomRuntimeSmoke[];
}

export interface CommandRoomDevelopmentNotice {
  id: string;
  title: string;
  status: string;
  priority: string;
  from: string;
  owner: string;
  worker: string;
  summary: string;
  request: string;
  response_path?: string;
  approval_boundary?: string;
  context_refs?: string[];
  source_path: string;
  provenance: string;
}

export interface CommandRoomDevelopmentWorkItem {
  id: string;
  kind?: string;
  title: string;
  status: string;
  priority: string;
  lane?: string;
  owner: string;
  summary: string;
  next_action: string;
  source_refs?: string[];
  updated?: string;
  source_path: string;
  provenance: string;
}

export interface CommandRoomRuntimeSmoke {
  id: string;
  title: string;
  status: string;
  summary: string;
  source_path: string;
  facts: CommandRoomFact[];
  findings?: string[];
  provenance: string;
}

export interface CommandRoomFact {
  label: string;
  value: string;
  provenance: string;
  source_path?: string;
}

export interface CommandRoomZeroClawAgent {
  id: string;
  name: string;
  display_name: string;
  status: string;
  hierarchy_role?: string;
  project_id?: string;
  parent_id?: string;
  port: number;
  config_path?: string;
  workspace_path?: string;
  created_by?: string;
  health_status?: string;
  last_seen?: string;
  provenance: string;
}

export interface CommandRoomDataSource {
  label: string;
  path?: string;
  status: string;
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

// --- Commander chat types ---

export interface CommanderDelta          { type: "delta"; text: string }
export interface CommanderToolCall       { type: "tool_call"; id: string; name: string; args: Record<string, unknown> }
export interface CommanderToolResult     { type: "tool_result"; id: string; name: string; output: string; error?: boolean }
export interface CommanderMessage        { type: "message"; role: string; content: string }
export interface CommanderDone           { type: "done"; input_tokens?: number; output_tokens?: number; context_tokens?: number; context_window?: number }
export interface CommanderError          { type: "error"; error: string }
export interface CommanderConfirmRequired { type: "confirm_required"; id: string; tool: string; args: Record<string, unknown>; summary: string }

export type CommanderEvent =
  | CommanderDelta
  | CommanderToolCall
  | CommanderToolResult
  | CommanderMessage
  | CommanderDone
  | CommanderError
  | CommanderConfirmRequired;

export interface CommanderHistoryMessage {
  role: string;
  content: string;
}

export interface MemoryEntry {
  key: string;
  value: string;
  created_at: string;
  updated_at: string;
}

export const FRAMEWORK_EMOJI: Record<string, string> = {
  zeroclaw: "🌀",
  openclaw: "🦞",
  hermes: "🔱",
  picoclaw: "🎯",
  embedded: "⚡",
};
