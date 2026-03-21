import { useState, useEffect, useCallback, useRef } from 'react';
import { Agent, AgentCommand, AgentConfigPayload, AgentConfigRecord, CommandResponse, CommandType, CommandHistory, AgentGroupData, CommandConfig, ControlSessionResponse, IPVersion } from '../types/yals';

interface UseYalsClientOptions {
  serverUrl?: string;
  maxReconnectAttempts?: number;
  reconnectDelay?: number;
}

const getLocalStorage = (key: string): string | null => {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
};

export const useYalsClient = (options: UseYalsClientOptions = {}) => {
  const [isConnected, setIsConnected] = useState(false);
  const [groups, setGroups] = useState<AgentGroupData>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [appConfig, setAppConfig] = useState<{ version: string; config: Record<string, string> } | null>(null);
  const [isConnecting, setIsConnecting] = useState(false);
  const [commands, setCommands] = useState<CommandConfig[]>(() => {
    try {
      const stored = getLocalStorage('yals_commands');
      return stored ? JSON.parse(stored) : [];
    } catch {
      return [];
    }
  });
  const [commandHistory, setCommandHistory] = useState<CommandHistory[]>(() => {
    try {
      const stored = getLocalStorage('yals_command_history');
      return stored ? JSON.parse(stored) : [];
    } catch {
      return [];
    }
  });
  const [activeCommands, setActiveCommands] = useState<Set<string>>(new Set());
  const [streamingOutputs, setStreamingOutputs] = useState<Map<string, string>>(new Map());
  const [abortControllers, setAbortControllers] = useState<Map<string, AbortController>>(new Map());
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [controlToken, setControlToken] = useState<string | null>(() => sessionStorage.getItem('yals_control_token'));
  const [isControlAuthenticated, setIsControlAuthenticated] = useState<boolean>(() => !!sessionStorage.getItem('yals_control_token'));
  const [managedAgents, setManagedAgents] = useState<AgentConfigRecord[]>([]);

  const reconnectAttemptsRef = useRef(0);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const {
    serverUrl = window.location.host,
    maxReconnectAttempts = 10,
    reconnectDelay = 1000
  } = options;

  const isControlPage = window.location.pathname === '/control.html';
  const protocol = window.location.protocol;

  const setLocalStorage = useCallback((key: string, value: string) => {
    try {
      localStorage.setItem(key, value);
    } catch (error) {
      console.warn('Unable to save to local storage:', error);
    }
  }, []);

  const buildHeaders = useCallback((headers: Record<string, string> = {}): HeadersInit => headers, []);

  const fetchNodesData = useCallback(async (sessionIdParam: string) => {
    const response = await fetch(`${protocol}//${serverUrl}/api/node?session_id=${sessionIdParam}`, {
      method: 'GET',
      headers: buildHeaders({ Accept: 'application/json' })
    });

    if (!response.ok) {
      throw new Error(`Failed to fetch nodes: ${response.status}`);
    }

    const data = await response.json();
    setAppConfig({
      version: data.version,
      config: {
        agents_total: data.total_nodes.toString(),
        agents_online: data.online_nodes.toString(),
        agents_offline: data.offline_nodes.toString()
      }
    });

    setGroups(data.groups || []);

    const allAgents: Agent[] = [];
    if (Array.isArray(data.groups)) {
      data.groups.forEach((group: any) => {
        if (Array.isArray(group.agents)) {
          allAgents.push(...group.agents);
        }
      });
    }
    setAgents(allAgents);

    if (!selectedAgent && allAgents.length > 0) {
      const preferredAgent = allAgents.find((agent: Agent) => agent.status === 1) || allAgents[0];
      setSelectedAgent(preferredAgent.name);
      const agentCommands = preferredAgent.commands as AgentCommand[] | undefined;
      if (agentCommands && Array.isArray(agentCommands)) {
        const commandsList: CommandConfig[] = agentCommands.map((cmd: AgentCommand) => ({
          name: cmd.name,
          description: cmd.description || cmd.name,
          template: cmd.template || '',
          use_plugin: cmd.use_plugin,
          ignore_target: cmd.ignore_target || false,
          maxmium_queue: cmd.maxmium_queue
        }));
        setCommands(commandsList);
        setLocalStorage('yals_commands', JSON.stringify(commandsList));
      }
    }
  }, [buildHeaders, protocol, serverUrl, selectedAgent, setLocalStorage]);

  const connect = useCallback(() => {
    setIsConnecting(true);

    return new Promise<void>(async (resolve, reject) => {
      try {
        let currentSessionId = sessionId || sessionStorage.getItem('yals_session_id');

        if (!currentSessionId) {
          const response = await fetch(`${protocol}//${serverUrl}/api/session`, {
            method: 'GET',
            headers: buildHeaders({ Accept: 'application/json' })
          });

          if (!response.ok) {
            throw new Error(`Failed to get session ID from server: ${response.status}`);
          }

          const sessionData = await response.json();
          currentSessionId = sessionData.session_id as string;
          if (!currentSessionId) {
            throw new Error('No session ID available');
          }
          sessionStorage.setItem('yals_session_id', currentSessionId);
        }

        setSessionId(currentSessionId);
        await fetchNodesData(currentSessionId);
        setIsConnected(true);
        setIsConnecting(false);
        reconnectAttemptsRef.current = 0;

        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
          reconnectTimeoutRef.current = null;
        }

        resolve();
      } catch (error) {
        console.error('YALS: Connection error:', error);
        setIsConnecting(false);
        setIsConnected(false);

        if (reconnectAttemptsRef.current < maxReconnectAttempts) {
          reconnectAttemptsRef.current++;
          const delay = Math.min(30000, reconnectDelay * Math.pow(1.5, reconnectAttemptsRef.current - 1));
          reconnectTimeoutRef.current = setTimeout(() => connect(), delay);
        }

        reject(error);
      }
    });
  }, [buildHeaders, fetchNodesData, maxReconnectAttempts, protocol, reconnectDelay, serverUrl, sessionId]);

  useEffect(() => {
    if (selectedAgent) {
      const agent = agents.find((item) => item.name === selectedAgent);
      const agentCommands = agent?.commands as AgentCommand[] | undefined;
      if (agentCommands && Array.isArray(agentCommands)) {
        const commandsList: CommandConfig[] = agentCommands.map((cmd: AgentCommand) => ({
          name: cmd.name,
          description: cmd.description || cmd.name,
          template: cmd.template || '',
          use_plugin: cmd.use_plugin,
          ignore_target: cmd.ignore_target || false,
          maxmium_queue: cmd.maxmium_queue
        }));
        setCommands(commandsList);
        setLocalStorage('yals_commands', JSON.stringify(commandsList));
      }
    }
  }, [agents, selectedAgent, setLocalStorage]);

  const clearAllStreamingOutputs = useCallback(() => {
    setStreamingOutputs(new Map());
  }, []);

  const stopCommand = useCallback(async (commandId: string) => {
    const currentSessionId = sessionId || sessionStorage.getItem('yals_session_id');
    if (!currentSessionId) return;

    await fetch(`${protocol}//${serverUrl}/api/stop?session_id=${currentSessionId}`, {
      method: 'POST',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ command_id: commandId })
    });

    const controller = abortControllers.get(commandId);
    if (controller) {
      controller.abort();
    }
  }, [abortControllers, buildHeaders, protocol, serverUrl, sessionId]);

  const executeCommand = useCallback(async (command: CommandType, target: string, ipVersion: IPVersion = 'auto'): Promise<{ response: CommandResponse; realCommandId: string }> => {
    if (!isConnected) {
      throw new Error('Not connected to server');
    }
    if (!selectedAgent) {
      throw new Error('No node selected');
    }

    const currentSessionId = sessionId || sessionStorage.getItem('yals_session_id');
    if (!currentSessionId) {
      throw new Error('No session ID available, please refresh the page.');
    }

    const commandConfig = commands.find((cmd) => cmd.name === command);
    const requiresTarget = !commandConfig?.ignore_target;
    if (requiresTarget && (!target || target.trim() === '')) {
      throw new Error('Target cannot be empty');
    }

    const trimmedTarget = requiresTarget ? target.trim() : '';
    const simpleCommandId = `${command}-${trimmedTarget}-${selectedAgent}-${currentSessionId}`;
    setActiveCommands((prev) => new Set(prev).add(simpleCommandId));

    const historyEntry: CommandHistory = {
      id: simpleCommandId,
      command,
      target: trimmedTarget,
      agent: selectedAgent,
      timestamp: Date.now(),
      ip_version: ipVersion
    };
    setCommandHistory((prev) => [historyEntry, ...prev.filter((h) => h.id !== simpleCommandId)]);

    const execUrl = `${protocol}//${serverUrl}/api/exec?session_id=${currentSessionId}`;

    return new Promise((resolve, reject) => {
      let accumulatedOutput = '';
      const abortController = new AbortController();
      setAbortControllers((prev) => new Map(prev).set(simpleCommandId, abortController));

      fetch(execUrl, {
        method: 'POST',
        headers: buildHeaders({
          'Content-Type': 'application/json',
          Accept: 'text/event-stream'
        }),
        body: JSON.stringify({
          agent: selectedAgent,
          command,
          target: trimmedTarget,
          ip_version: ipVersion
        }),
        signal: abortController.signal
      }).then(async (response) => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        if (!response.body) {
          throw new Error('Response body is null');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();

        while (true) {
          const { done, value } = await reader.read();
          if (done) {
            break;
          }

          const chunk = decoder.decode(value, { stream: true });
          const lines = chunk.split('\n');
          for (const line of lines) {
            if (!line.startsWith('data: ')) continue;
            try {
              const message = JSON.parse(line.substring(6));
              if (message.type === 'output') {
                accumulatedOutput = message.output || '';
                setStreamingOutputs((prev) => new Map(prev).set(simpleCommandId, accumulatedOutput));
              } else if (message.type === 'error') {
                accumulatedOutput = message.error || '';
                setStreamingOutputs((prev) => new Map(prev).set(simpleCommandId, accumulatedOutput));
              } else if (message.type === 'complete') {
                const commandResponse: CommandResponse = {
                  success: message.success,
                  command,
                  target: trimmedTarget,
                  agent: selectedAgent,
                  output: accumulatedOutput,
                  error: message.error,
                  timestamp: Date.now(),
                  stopped: message.stopped || false
                };

                setCommandHistory((prev) => {
                  const existingIndex = prev.findIndex((h) => h.id === simpleCommandId);
                  if (existingIndex >= 0) {
                    const updated = [...prev];
                    updated[existingIndex] = { ...updated[existingIndex], response: commandResponse };
                    setLocalStorage('yals_command_history', JSON.stringify(updated.slice(0, 100)));
                    return updated;
                  }
                  return prev;
                });

                setActiveCommands((prev) => {
                  const next = new Set(prev);
                  next.delete(simpleCommandId);
                  return next;
                });
                setAbortControllers((prev) => {
                  const next = new Map(prev);
                  next.delete(simpleCommandId);
                  return next;
                });

                if (message.success || message.stopped) {
                  resolve({ response: commandResponse, realCommandId: simpleCommandId });
                } else {
                  reject(new Error(message.error || 'Command execution failed'));
                }
              }
            } catch (error) {
              console.error('Failed to parse SSE message:', error);
            }
          }
        }
      }).catch((error) => {
        setActiveCommands((prev) => {
          const next = new Set(prev);
          next.delete(simpleCommandId);
          return next;
        });
        setAbortControllers((prev) => {
          const next = new Map(prev);
          next.delete(simpleCommandId);
          return next;
        });
        reject(error);
      });
    });
  }, [buildHeaders, commands, isConnected, protocol, selectedAgent, serverUrl, sessionId, setLocalStorage]);

  const controlHeaders = useCallback((): Record<string, string> => {
    const token = controlToken || sessionStorage.getItem('yals_control_token');
    return token ? { Authorization: `Bearer ${token}` } : {};
  }, [controlToken]);

  const loginControl = useCallback(async (password: string) => {
    const response = await fetch(`${protocol}//${serverUrl}/api/control/login`, {
      method: 'POST',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ password })
    });

    if (!response.ok) {
      throw new Error('控制台密码错误');
    }

    const data = await response.json() as ControlSessionResponse;
    if (!data.token) {
      throw new Error('控制台登录失败');
    }

    sessionStorage.setItem('yals_control_token', data.token);
    setControlToken(data.token);
    setIsControlAuthenticated(true);
    return data;
  }, [buildHeaders, protocol, serverUrl]);

  const validateControlSession = useCallback(async () => {
    const token = controlToken || sessionStorage.getItem('yals_control_token');
    if (!token) {
      setIsControlAuthenticated(false);
      return false;
    }

    const response = await fetch(`${protocol}//${serverUrl}/api/control/session`, {
      method: 'GET',
      headers: buildHeaders({
        Accept: 'application/json',
        Authorization: `Bearer ${token}`
      })
    });

    if (!response.ok) {
      sessionStorage.removeItem('yals_control_token');
      setControlToken(null);
      setIsControlAuthenticated(false);
      return false;
    }

    setIsControlAuthenticated(true);
    return true;
  }, [buildHeaders, controlToken, protocol, serverUrl]);

  const listManagedAgents = useCallback(async () => {
    const response = await fetch(`${protocol}//${serverUrl}/api/control/agents`, {
      method: 'GET',
      headers: buildHeaders({
        Accept: 'application/json',
        ...controlHeaders()
      })
    });

    if (!response.ok) {
      throw new Error('获取 Agent 列表失败');
    }

    const data = await response.json() as AgentConfigRecord[];
    setManagedAgents(data);
    return data;
  }, [buildHeaders, controlHeaders, protocol, serverUrl]);

  const saveManagedAgent = useCallback(async (payload: AgentConfigPayload) => {
    const isUpdate = !!payload.uuid;
    const url = isUpdate
      ? `${protocol}//${serverUrl}/api/control/agents/${payload.uuid}`
      : `${protocol}//${serverUrl}/api/control/agents`;

    const response = await fetch(url, {
      method: isUpdate ? 'PUT' : 'POST',
      headers: buildHeaders({
        'Content-Type': 'application/json',
        ...controlHeaders()
      }),
      body: JSON.stringify(payload)
    });

    if (!response.ok) {
      throw new Error((await response.text()) || '保存 Agent 失败');
    }

    const data = await response.json() as AgentConfigRecord;
    await listManagedAgents();
    return data;
  }, [buildHeaders, controlHeaders, listManagedAgents, protocol, serverUrl]);

  const deleteManagedAgent = useCallback(async (uuid: string) => {
    const response = await fetch(`${protocol}//${serverUrl}/api/control/agents/${uuid}`, {
      method: 'DELETE',
      headers: buildHeaders(controlHeaders())
    });

    if (!response.ok) {
      throw new Error('删除 Agent 失败');
    }

    await listManagedAgents();
  }, [buildHeaders, controlHeaders, listManagedAgents, protocol, serverUrl]);

  useEffect(() => {
    if (isControlPage) {
      validateControlSession().catch(() => setIsControlAuthenticated(false));
    }
  }, [isControlPage, validateControlSession]);

  return {
    isConnected,
    isConnecting,
    groups,
    agents,
    selectedAgent,
    activeCommands,
    streamingOutputs,
    appConfig,
    commands,
    commandHistory,
    connect,
    executeCommand,
    setSelectedAgent,
    clearAllStreamingOutputs,
    stopCommand,
    isControlAuthenticated,
    managedAgents,
    loginControl,
    validateControlSession,
    listManagedAgents,
    saveManagedAgent,
    deleteManagedAgent
  };
};
