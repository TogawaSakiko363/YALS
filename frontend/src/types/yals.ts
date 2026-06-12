export interface AgentDetails {
  location: string;
  datacenter: string;
  test_ip: string;
  description: string;
}

export interface AgentCommand {
  name: string;
  template?: string;
  use_plugin?: string;
  ignore_target?: boolean;
  maxmium_queue?: number;
}

export interface Agent {
  uuid?: string;
  name: string;
  status: number;
  location?: string;
  description?: string;
  details?: AgentDetails;
  commands?: AgentCommand[];
}

export interface CommandResponse {
  success: boolean;
  command: string;
  target: string;
  agent: string;
  output?: string;
  error?: string;
  timestamp?: number;
  stopped?: boolean;
  ip_version?: string;
}

export interface AgentGroup {
  [groupName: string]: Agent[];
}

export interface GroupData {
  name: string;
  agents: Agent[];
}

export type AgentGroupData = AgentGroup | GroupData[];

export interface YalsMessage {
  type?: string;
  agents?: Agent[];
  groups?: AgentGroup;
  command?: string;
  target?: string;
  agent?: string;
  success?: boolean;
  output?: string;
  error?: string;
  is_complete?: boolean;
  stopped?: boolean;
}

export type CommandType = string;

export interface CommandConfig {
  name: string;
  template: string;
  use_plugin?: string;
  ignore_target?: boolean;
  maxmium_queue?: number;
}

export interface CommandsResponse {
  commands: Record<string, string>;
}

export interface CommandHistory {
  id: string;
  command: CommandType;
  target: string;
  agent: string;
  timestamp: number;
  response?: CommandResponse;
  ip_version?: string;
}

export interface ControlSessionResponse {
  authenticated: boolean;
  token?: string;
}

export interface RuntimeSettings {
  grpc: {
    ping_interval: number;
    pong_wait: number;
  };
  rate_limit: {
    enabled: boolean;
    max_commands: number;
    time_window: number;
  };
}

export interface AgentConfigPayload {
  uuid?: string;
  token: string;
  name: string;
  group: string;
  details: AgentDetails;
  commands: AgentCommand[];
}

export interface AgentConfigRecord extends AgentConfigPayload {
  uuid: string;
  created_at: string;
  updated_at: string;
}

export type IPVersion = 'auto' | 'ipv4' | 'ipv6';

export interface PluginInfo {
  name: string;
  description: string;
  ignore_target: boolean;
  ignore_target_overridden: boolean;
  maximum_queue: number;
  maximum_queue_overridden: boolean;
}

export interface AgentSystemMetrics {
  updated_at: string;
  cpu_percent: number;
  mem_used: number;
  mem_total: number;
  disk_used: number;
  disk_total: number;
  net_up_rate: number;
  net_down_rate: number;
  net_up_total: number;
  net_down_total: number;
  uptime_sec: number;
}

export interface StatusItem {
  uuid: string;
  name: string;
  group: string;
  online: boolean;
  metrics?: AgentSystemMetrics;
}

export interface ProbeRow {
  name: string;
  location: string;
  isp: string;
  protocol: string;
  has_data: boolean;
  latest_ms: number;
  has_latest: boolean;
  avg_ms: number;
  has_avg: boolean;
  loss_pct: number;
}

export interface ProbeTarget {
  ip: string;
  name: string;
  location: string;
  isp: string;
  protocol: string;
}

export interface ProbeConfigPayload {
  interval_sec: number;
  targets: ProbeTarget[];
}
