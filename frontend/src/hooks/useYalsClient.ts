import { useState, useEffect, useCallback, useRef } from 'react';
import { Agent, CommandResponse, CommandType, CommandHistory, AgentGroupData } from '../types/yals';

interface UseYalsClientOptions {
  serverUrl?: string;
  maxReconnectAttempts?: number;
  reconnectDelay?: number;
}

export const useYalsClient = (options: UseYalsClientOptions = {}) => {
  const [isConnected, setIsConnected] = useState(false);
  const [groups, setGroups] = useState<AgentGroupData>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [appConfig, setAppConfig] = useState<{ version: string; config: Record<string, string> } | null>(null);
  const [isConnecting, setIsConnecting] = useState(false);
  const [commands, setCommands] = useState<Record<string, string>>(() => {
    try {
      const stored = getLocalStorage('yals_commands');
      return stored ? JSON.parse(stored) : {};
    } catch {
      return {};
    }
  });

  const getLocalStorage = (key: string): string | null => {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  };
  
  const setLocalStorage = (key: string, value: string) => {
    try {
      localStorage.setItem(key, value);
    } catch (error) {
      console.warn('无法保存到本地存储:', error);
    }
  };

  const [commandHistory, setCommandHistory] = useState<CommandHistory[]>(() => {
    try {
      const stored = getLocalStorage('yals_command_history');
      return stored ? JSON.parse(stored) : [];
    } catch {
      return [];
    }
  });
  const [activeCommands, setActiveCommands] = useState<Set<string>>(new Set());

  const socketRef = useRef<WebSocket | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const {
    serverUrl = window.location.host,
    maxReconnectAttempts = 10,
    reconnectDelay = 1000
  } = options;

  const handleReconnect = useCallback(() => {
    if (reconnectAttemptsRef.current < maxReconnectAttempts) {
      reconnectAttemptsRef.current++;
      
      const delay = Math.min(30000, reconnectDelay * Math.pow(1.5, reconnectAttemptsRef.current - 1));
      
      console.log(`YALS: 尝试在${delay}毫秒后重新连接 (尝试 ${reconnectAttemptsRef.current}/${maxReconnectAttempts})`);
      
      reconnectTimeoutRef.current = setTimeout(() => {
        connect();
      }, delay);
    } else {
      console.error('YALS: 达到最大重连尝试次数，停止重连');
    }
  }, [maxReconnectAttempts, reconnectDelay]);

  const handleMessage = useCallback((data: string) => {
    try {
      const message = JSON.parse(data);
      
      if (message.type === 'agent_status') {
          const groupsData = message.groups || {};
          setGroups(groupsData);
          
          const allAgents: Agent[] = [];
          
          if (Array.isArray(groupsData)) {
            groupsData.forEach(group => {
              if (Array.isArray(group.agents)) {
                allAgents.push(...group.agents);
              }
            });
          } else {
            Object.values(groupsData).forEach((value: unknown) => {
              if (Array.isArray(value)) {
                const groupAgents = value as Agent[];
                allAgents.push(...groupAgents);
              }
            });
          }
          
          setAgents(allAgents);
          
          if (!selectedAgent && allAgents.length > 0) {
            const onlineAgent = allAgents.find((agent: Agent) => agent.status === 1);
            if (onlineAgent) {
              setSelectedAgent(onlineAgent.name);
            }
          }
        } else if (message.type === 'app_config') {
        setAppConfig({
          version: message.version,
          config: message.config
        });
      } else if (message.type === 'commands_list') {
        const commandsArray = message.commands || [];
        const commandsMap: Record<string, string> = {};
        
        commandsArray.forEach((cmd: { name: string; description: string }) => {
          commandsMap[cmd.name] = cmd.description;
        });
        
        setCommands(commandsMap);
        setLocalStorage('yals_commands', JSON.stringify(commandsMap));
       } else if (message.command) {
        const commandId = `${message.command}-${message.target}-${message.agent}`;
        
        setActiveCommands(prev => {
          const newSet = new Set(prev);
          newSet.delete(commandId);
          return newSet;
        });

        setCommandHistory(prev => {
          const existingIndex = prev.findIndex(h => h.id === commandId);
          
          const response: CommandResponse = {
            success: message.success,
            command: message.command,
            target: message.target,
            agent: message.agent,
            output: message.output,
            error: message.error,
            timestamp: Date.now()
          };

          let updatedHistory;
          if (existingIndex >= 0) {
            const updated = [...prev];
            updated[existingIndex] = { ...updated[existingIndex], response };
            updatedHistory = updated;
          } else {
            const newHistoryItem: CommandHistory = {
              id: commandId,
              command: message.command,
              target: message.target,
              agent: message.agent,
              timestamp: Date.now(),
              response
            };
            updatedHistory = [newHistoryItem, ...prev];
          }
          
          const limitedHistory = updatedHistory.slice(0, 100);
          
          setLocalStorage('yals_command_history', JSON.stringify(limitedHistory));
          
          return limitedHistory;
        });
      }
    } catch (error) {
      console.error('YALS: 解析消息错误:', error);
    }
  }, [selectedAgent]);

  const connect = useCallback(() => {
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      return Promise.resolve();
    }

    setIsConnecting(true);

    return new Promise<void>((resolve, reject) => {
      try {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${serverUrl}/ws`;
        
        const socket = new WebSocket(wsUrl);
        
        socket.onopen = () => {
          console.log('YALS: WebSocket连接已建立');
          socketRef.current = socket;
          setIsConnected(true);
          setIsConnecting(false);
          reconnectAttemptsRef.current = 0;
          
          if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current);
            reconnectTimeoutRef.current = null;
          }
          
          const configRequest = JSON.stringify({ type: 'get_config' });
          socket.send(configRequest);
          
          const commandsRequest = JSON.stringify({ type: 'get_commands' });
          socket.send(commandsRequest);
          
          resolve();
        };
        
        socket.onclose = () => {
          console.log('YALS: WebSocket连接已关闭');
          setIsConnected(false);
          setIsConnecting(false);
          socketRef.current = null;
          
          handleReconnect();
        };
        
        socket.onerror = (error) => {
          console.error('YALS: WebSocket错误:', error);
          setIsConnecting(false);
          reject(error);
        };
        
        socket.onmessage = (event) => {
          handleMessage(event.data);
        };
      } catch (error) {
        console.error('YALS: 连接错误:', error);
        setIsConnecting(false);
        reject(error);
      }
    });
  }, [serverUrl, handleMessage, handleReconnect]);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
    
    setIsConnected(false);
    setIsConnecting(false);
    reconnectAttemptsRef.current = maxReconnectAttempts;
  }, [maxReconnectAttempts]);

  const executeCommand = useCallback(async (command: CommandType, target: string): Promise<CommandResponse> => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket未连接');
    }
    
    if (!selectedAgent) {
      throw new Error('未选择代理');
    }
    
    if (!target || target.trim() === '') {
      throw new Error('目标不能为空');
    }

    const trimmedTarget = target.trim();
    const commandId = `${command}-${trimmedTarget}-${selectedAgent}`;
    
    setActiveCommands(prev => new Set(prev).add(commandId));
    
    const historyEntry: CommandHistory = {
      id: commandId,
      command,
      target: trimmedTarget,
      agent: selectedAgent,
      timestamp: Date.now()
    };
    
    setCommandHistory(prev => [historyEntry, ...prev.filter(h => h.id !== commandId)]);

    const request = {
      type: 'execute_command',
      agent: selectedAgent,
      command,
      target: trimmedTarget
    };

    return new Promise((resolve, reject) => {
      const timeoutId = setTimeout(() => {
        setActiveCommands(prev => {
          const newSet = new Set(prev);
          newSet.delete(commandId);
          return newSet;
        });
        reject(new Error('命令执行超时'));
      }, 60000); // 60秒超时

      const messageHandler = (event: MessageEvent) => {
        try {
          const response = JSON.parse(event.data);
          
          if (response.command === command && 
              response.agent === selectedAgent && 
              response.target === trimmedTarget) {
            
            socketRef.current?.removeEventListener('message', messageHandler);
            clearTimeout(timeoutId);
            
            const commandResponse: CommandResponse = {
              success: response.success,
              command: response.command,
              target: response.target,
              agent: response.agent,
              output: response.output,
              error: response.error,
              timestamp: Date.now()
            };
            
            if (response.success) {
              resolve(commandResponse);
            } else {
              reject(new Error(response.error || '命令执行失败'));
            }
          }
        } catch (error) {
          // 忽略解析错误
        }
      };

      socketRef.current?.addEventListener('message', messageHandler);
      socketRef.current?.send(JSON.stringify(request));
    });
  }, [selectedAgent]);

  const clearHistory = useCallback(() => {
    setCommandHistory([]);
    setLocalStorage('yals_command_history', '[]');
  }, []);

  useEffect(() => {
    try {
      document.cookie = `yals_command_history=${encodeURIComponent(JSON.stringify(commandHistory))}`;
    } catch {}
  }, [commandHistory]);

  useEffect(() => {
    return () => {
      disconnect();
    };
  }, [disconnect]);

  return {
    isConnected,
    isConnecting,
    groups,
    agents,
    selectedAgent,
    setSelectedAgent,
    appConfig,
    commands,
    commandHistory,
    activeCommands,
    connect,
    disconnect,
    executeCommand,
    clearHistory
  };
};