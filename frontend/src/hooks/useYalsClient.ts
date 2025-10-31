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
  // 本地存储工具函数
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
  const [streamingOutputs, setStreamingOutputs] = useState<Map<string, string>>(new Map());

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
          
          // 将分组数据转换为agents列表用于向后兼容
          const allAgents: Agent[] = [];
          
          if (Array.isArray(groupsData)) {
            // 新格式：有序数组
            groupsData.forEach(group => {
              if (Array.isArray(group.agents)) {
                allAgents.push(...group.agents);
              }
            });
          } else {
            // 旧格式：对象
            Object.values(groupsData).forEach((value: unknown) => {
              if (Array.isArray(value)) {
                const groupAgents = value as Agent[];
                allAgents.push(...groupAgents);
              }
            });
          }
          
          setAgents(allAgents);
          
          // 如果没有选择节点且有可用节点，选择第一个在线的节点
          if (!selectedAgent && allAgents.length > 0) {
            const onlineAgent = allAgents.find((agent: Agent) => agent.status === 1);
            if (onlineAgent) {
              setSelectedAgent(onlineAgent.name);
              // 获取该agent的commands
              getAgentCommands(onlineAgent.name);
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
        
        // 将数组转换为对象，保持服务器返回的顺序
        commandsArray.forEach((cmd: { name: string; description: string }) => {
          commandsMap[cmd.name] = cmd.description;
        });
        
        setCommands(commandsMap);
        setLocalStorage('yals_commands', JSON.stringify(commandsMap));
      } else if (message.type === 'command_output') {
        // Handle streaming command output
        const commandId = `${message.command}-${message.target}-${message.agent}`;
        
        if (message.is_complete) {
          // Command completed
          setActiveCommands(prev => {
            const newSet = new Set(prev);
            newSet.delete(commandId);
            return newSet;
          });
          
          // Don't update command history here, let executeCommand handle it
          // The history will be updated when executeCommand resolves with the final output
        } else {
          // Streaming output chunk
          setStreamingOutputs(prev => {
            const newMap = new Map(prev);
            const currentOutput = newMap.get(commandId) || '';
            const newOutput = message.error ? 
              currentOutput + (currentOutput ? '\n' : '') + message.error :
              currentOutput + (currentOutput ? '\n' : '') + message.output;
            newMap.set(commandId, newOutput);
            return newMap;
          });
        }
      } else if (message.command) {
        // 处理命令响应
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
            // 如果是新命令，添加到历史记录
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
          
          // 限制历史记录数量，避免过大
          const limitedHistory = updatedHistory.slice(0, 100);
          
          // 立即保存到本地存储
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
          
          // 获取应用配置
          const configRequest = JSON.stringify({ type: 'get_config' });
          socket.send(configRequest);
          
          // 不再自动获取全局commands，而是在选择agent时获取该agent的commands
          
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
    reconnectAttemptsRef.current = maxReconnectAttempts; // 防止自动重连
  }, [maxReconnectAttempts]);

  const executeCommand = useCallback(async (command: CommandType, target: string): Promise<CommandResponse> => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket未连接');
    }
    
    if (!selectedAgent) {
      throw new Error('未选择节点');
    }
    
    if (!target || target.trim() === '') {
      throw new Error('目标不能为空');
    }

    const trimmedTarget = target.trim();
    const commandId = `${command}-${trimmedTarget}-${selectedAgent}`;
    
    // 添加到活动命令集合
    setActiveCommands(prev => new Set(prev).add(commandId));
    
    // 清理之前的流式输出
    setStreamingOutputs(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });
    
    // 添加到历史记录
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
      let accumulatedOutput = ''; // 在Promise内部累积输出
      
      const timeoutId = setTimeout(() => {
        setActiveCommands(prev => {
          const newSet = new Set(prev);
          newSet.delete(commandId);
          return newSet;
        });
        setStreamingOutputs(prev => {
          const newMap = new Map(prev);
          newMap.delete(commandId);
          return newMap;
        });
        reject(new Error('命令执行超时'));
      }, 120000); // 120秒超时，给流式输出更多时间

      const messageHandler = (event: MessageEvent) => {
        try {
          const response = JSON.parse(event.data);
          
          // Handle streaming output chunks
          if (response.type === 'command_output' && 
              response.command === command && 
              response.agent === selectedAgent && 
              response.target === trimmedTarget &&
              !response.is_complete) {
            
            // 累积输出
            if (response.output) {
              accumulatedOutput += (accumulatedOutput ? '\n' : '') + response.output;
            }
            if (response.error) {
              accumulatedOutput += (accumulatedOutput ? '\n' : '') + response.error;
            }
          }
          
          // Handle streaming output completion
          else if (response.type === 'command_output' && 
              response.command === command && 
              response.agent === selectedAgent && 
              response.target === trimmedTarget &&
              response.is_complete) {
            
            socketRef.current?.removeEventListener('message', messageHandler);
            clearTimeout(timeoutId);
            
            const commandResponse: CommandResponse = {
              success: response.success,
              command: response.command,
              target: response.target,
              agent: response.agent,
              output: accumulatedOutput, // 使用累积的输出
              error: response.error,
              timestamp: Date.now()
            };
            
            // Update command history with final result
            setCommandHistory(prev => {
              const existingIndex = prev.findIndex(h => h.id === commandId);
              if (existingIndex >= 0) {
                const updated = [...prev];
                // 检查是否是停止状态
                const finalResponse = {
                  ...commandResponse,
                  // 如果错误信息是"已取消"，标记为取消状态
                  output: commandResponse.error === '已取消' ? 
                    (accumulatedOutput + '\n*** Stopped ***') : 
                    commandResponse.output
                };
                
                updated[existingIndex] = { 
                  ...updated[existingIndex], 
                  response: finalResponse
                };
                
                // Save to localStorage
                setLocalStorage('yals_command_history', JSON.stringify(updated.slice(0, 100)));
                return updated;
              }
              return prev;
            });
            
            if (response.success) {
              resolve(commandResponse);
            } else {
              reject(new Error(response.error || '命令执行失败'));
            }
          }
          
          // Handle legacy non-streaming response (fallback)
          else if (response.command === command && 
              response.agent === selectedAgent && 
              response.target === trimmedTarget &&
              !response.type) {
            
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

  const clearStreamingOutput = useCallback((commandId: string) => {
    setStreamingOutputs(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });
  }, []);

  const getAgentCommands = useCallback((agentName: string) => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      return;
    }

    const request = {
      type: 'get_agent_commands',
      agent: agentName
    };

    socketRef.current.send(JSON.stringify(request));
  }, []);

  const stopCommand = useCallback((commandId: string) => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      return;
    }

    // 发送停止命令到后端
    const stopRequest = {
      type: 'stop_command',
      command_id: commandId
    };

    socketRef.current.send(JSON.stringify(stopRequest));
    
    // 注意：不要立即清理状态，等待后端返回停止确认
  }, []);

  // 当历史更新时写入 Cookie（4KB 限制，历史过大可能被截断）
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

  const handleSetSelectedAgent = useCallback((agentName: string | null) => {
    setSelectedAgent(agentName);
    if (agentName) {
      getAgentCommands(agentName);
    } else {
      // 如果没有选择agent，清空commands
      setCommands({});
    }
  }, [getAgentCommands]);

  return {
    isConnected,
    isConnecting,
    groups,
    agents,
    selectedAgent,
    setSelectedAgent: handleSetSelectedAgent,
    appConfig,
    commands,
    commandHistory,
    activeCommands,
    streamingOutputs,
    connect,
    disconnect,
    executeCommand,
    clearHistory,
    clearStreamingOutput,
    stopCommand
  };
};