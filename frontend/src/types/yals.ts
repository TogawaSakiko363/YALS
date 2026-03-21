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
  description?: string;
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
  description: string;
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
