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
  // Local storage utility functions
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
      console.warn('Unable to save to local storage:', error);
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
      
      console.log(`YALS: Attempting reconnection in ${delay}ms (attempt ${reconnectAttemptsRef.current}/${maxReconnectAttempts})`);
      
      reconnectTimeoutRef.current = setTimeout(() => {
        connect();
      }, delay);
    } else {
      console.error('YALS: Maximum reconnection attempts reached, stopping reconnection');
    }
  }, [maxReconnectAttempts, reconnectDelay]);

  const handleMessage = useCallback((data: string) => {
    try {
      const message = JSON.parse(data);
      
      if (message.type === 'agent_status') {
          const groupsData = message.groups || {};
          setGroups(groupsData);
          
          // Convert group data to agents list for backward compatibility
          const allAgents: Agent[] = [];
          
          if (Array.isArray(groupsData)) {
            // New format: ordered array
            groupsData.forEach(group => {
              if (Array.isArray(group.agents)) {
                allAgents.push(...group.agents);
              }
            });
          } else {
            // Old format: object
            Object.values(groupsData).forEach((value: unknown) => {
              if (Array.isArray(value)) {
                const groupAgents = value as Agent[];
                allAgents.push(...groupAgents);
              }
            });
          }
          
          setAgents(allAgents);
          
          // If no node selected and nodes available, select first online node
          if (!selectedAgent && allAgents.length > 0) {
            const onlineAgent = allAgents.find((agent: Agent) => agent.status === 1);
            if (onlineAgent) {
              setSelectedAgent(onlineAgent.name);
              // Get commands for this agent
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
        
        // Convert array to object, maintaining server-returned order
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
        // Handle command response
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
            // If new command, add to history
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
          
          // Limit history size to avoid excessive growth
          const limitedHistory = updatedHistory.slice(0, 100);
          
          // Save to local storage immediately
          setLocalStorage('yals_command_history', JSON.stringify(limitedHistory));
          
          return limitedHistory;
        });
      }
    } catch (error) {
      console.error('YALS: Message parsing error:', error);
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
          console.log('YALS: WebSocket connection established');
          socketRef.current = socket;
          setIsConnected(true);
          setIsConnecting(false);
          reconnectAttemptsRef.current = 0;
          
          if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current);
            reconnectTimeoutRef.current = null;
          }
          
          // Get application config
          const configRequest = JSON.stringify({ type: 'get_config' });
          socket.send(configRequest);
          
          // No longer auto-fetch global commands, get agent-specific commands when selecting agent
          
          resolve();
        };
        
        socket.onclose = () => {
          console.log('YALS: WebSocket connection closed');
          setIsConnected(false);
          setIsConnecting(false);
          socketRef.current = null;
          
          handleReconnect();
        };
        
        socket.onerror = (error) => {
          console.error('YALS: WebSocket error:', error);
          setIsConnecting(false);
          reject(error);
        };
        
        socket.onmessage = (event) => {
          handleMessage(event.data);
        };
      } catch (error) {
        console.error('YALS: Connection error:', error);
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
    reconnectAttemptsRef.current = maxReconnectAttempts; // Prevent auto-reconnection
  }, [maxReconnectAttempts]);

  const executeCommand = useCallback(async (command: CommandType, target: string): Promise<CommandResponse> => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }
    
    if (!selectedAgent) {
      throw new Error('No node selected');
    }
    
    if (!target || target.trim() === '') {
      throw new Error('Target cannot be empty');
    }

    const trimmedTarget = target.trim();
    const commandId = `${command}-${trimmedTarget}-${selectedAgent}`;
    
    // Add to active commands set
    setActiveCommands(prev => new Set(prev).add(commandId));
    
    // Clear previous streaming output
    setStreamingOutputs(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });
    
    // Add to history
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
      let accumulatedOutput = ''; // Accumulate output within Promise
      
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
        reject(new Error('Command execution timeout'));
      }, 120000); // 120 second timeout, allow more time for streaming output

      const messageHandler = (event: MessageEvent) => {
        try {
          const response = JSON.parse(event.data);
          
          // Handle streaming output chunks
          if (response.type === 'command_output' && 
              response.command === command && 
              response.agent === selectedAgent && 
              response.target === trimmedTarget &&
              !response.is_complete) {
            
            // Accumulate output
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
              output: accumulatedOutput, // Use accumulated output
              error: response.error,
              timestamp: Date.now()
            };
            
            // Update command history with final result
            setCommandHistory(prev => {
              const existingIndex = prev.findIndex(h => h.id === commandId);
              if (existingIndex >= 0) {
                const updated = [...prev];
                // Check if it's a stopped state
                const finalResponse = {
                  ...commandResponse,
                  // If error message is "cancelled", mark as cancelled state
                  output: commandResponse.error === 'Cancelled' ? 
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
              reject(new Error(response.error || 'Command execution failed'));
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
              reject(new Error(response.error || 'Command execution failed'));
            }
          }
        } catch (error) {
          // Ignore parsing errors
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

    // Send stop command to backend
    const stopRequest = {
      type: 'stop_command',
      command_id: commandId
    };

    socketRef.current.send(JSON.stringify(stopRequest));
    
    // Note: Don't immediately clean up state, wait for backend stop confirmation
  }, []);

  // Write to Cookie when history updates (4KB limit, large history may be truncated)
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
      // If no agent selected, clear commands
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