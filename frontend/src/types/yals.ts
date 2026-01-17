export interface AgentDetails {
  location: string;
  datacenter: string;
  test_ip: string;
  description: string;
}

export interface AgentCommand {
  name: string;
  ignore_target?: boolean;
}

export interface Agent {
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
  stopped?: boolean; // Indicates if command was stopped by user
  ip_version?: string; // IP version used: "auto", "ipv4", or "ipv6"
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
  stopped?: boolean; // Indicates if command was stopped by user
}

export type CommandType = string;

export interface CommandConfig {
  name: string;
  description: string;
  template: string;
  ignore_target?: boolean;  // Whether target parameter is ignored
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
  ip_version?: string; // IP version: "auto", "ipv4", or "ipv6"
}

export type IPVersion = 'auto' | 'ipv4' | 'ipv6';