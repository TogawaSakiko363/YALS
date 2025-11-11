export interface AgentDetails {
  location: string;
  datacenter: string;
  test_ip: string;
  description: string;
}

export interface Agent {
  name: string;
  status: number;
  location?: string;
  description?: string;
  details?: AgentDetails;
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
  output_mode?: string; // "append" or "replace"
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
}