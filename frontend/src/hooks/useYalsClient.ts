import { useState, useEffect, useCallback, useRef } from 'react';
import { Agent, CommandResponse, CommandType, CommandHistory, AgentGroupData, CommandConfig, IPVersion } from '../types/yals';

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
  const [commands, setCommands] = useState<CommandConfig[]>(() => {
    try {
      const stored = getLocalStorage('yals_commands');
      return stored ? JSON.parse(stored) : [];
    } catch {
      return [];
    }
  });
  // Local storage utility functions with debouncing
  const getLocalStorage = (key: string): string | null => {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  };

  const setLocalStorage = useCallback((key: string, value: string) => {
    try {
      localStorage.setItem(key, value);
    } catch (error) {
      console.warn('Unable to save to local storage:', error);
    }
  }, []);

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
  const [commandIdMapping, setCommandIdMapping] = useState<Map<string, string>>(new Map()); // Map simple ID to real backend ID



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

        // Keep as array to maintain server-returned order
        const commandsList: CommandConfig[] = commandsArray.map((cmd: {
          name: string;
          description: string;
          ignore_target?: boolean;
        }) => ({
          name: cmd.name,
          description: cmd.description,
          template: '', // Template is not sent to frontend
          ignore_target: cmd.ignore_target || false
        }));

        setCommands(commandsList);
        setLocalStorage('yals_commands', JSON.stringify(commandsList));
      } else if (message.type === 'command_output') {
        // Handle streaming command output
        const commandId = `${message.command}-${message.target}-${message.agent}`;

        // Update streaming output first (even if complete, to handle stopped messages)
        if (message.output || message.error) {
          setStreamingOutputs(prev => {
            const newMap = new Map(prev);
            const currentOutput = newMap.get(commandId) || '';
            
            // Check output mode - default to "replace" for better user experience
            const outputMode = message.output_mode || 'replace';
            
            let newOutput: string;
            if (outputMode === 'replace') {
              // Replace mode: use new output directly
              newOutput = message.error || message.output || '';
            } else {
              // Append mode: accumulate output (for stopped messages)
              // Don't add extra newline if output already starts with newline
              const separator = message.output && message.output.startsWith('\n') ? '' : (currentOutput ? '\n' : '');
              newOutput = message.error ?
                currentOutput + separator + message.error :
                currentOutput + message.output;
            }
            
            newMap.set(commandId, newOutput);
            return newMap;
          });
        }

        if (message.is_complete) {
          // Command completed
          setActiveCommands(prev => {
            const newSet = new Set(prev);
            newSet.delete(commandId);
            return newSet;
          });

          // Don't update command history here, let executeCommand handle it
          // The history will be updated when executeCommand resolves with the final output
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

          // Save to local storage
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

    return new Promise<void>(async (resolve, reject) => {
      try {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const httpProtocol = window.location.protocol;
        
        // Fetch session ID from server API
        let sessionId = sessionStorage.getItem('yals_session_id');
        
        if (!sessionId) {
          const response = await fetch(`${httpProtocol}//${serverUrl}/api/session`, {
            method: 'GET',
            headers: {
              'Accept': 'application/json',
            },
          });

          if (!response.ok) {
            throw new Error(`Failed to get session ID from server: ${response.status}`);
          }

          const data = await response.json();
          sessionId = data.session_id;
          
          if (!sessionId) {
            throw new Error('Server did not return a valid session ID');
          }

          // Store session ID in sessionStorage
          sessionStorage.setItem('yals_session_id', sessionId);
          console.log('YALS: Received session ID from server:', sessionId);
        }
        
        // Include session ID in WebSocket URL path
        const wsUrl = `${protocol}//${serverUrl}/ws/${sessionId}`;

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

  const executeCommand = useCallback(async (command: CommandType, target: string, ipVersion: IPVersion = 'auto'): Promise<{ response: CommandResponse; realCommandId: string }> => {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }

    if (!selectedAgent) {
      throw new Error('No node selected');
    }

    // Check if command requires target
    const commandConfig = commands.find(cmd => cmd.name === command);
    const requiresTarget = !commandConfig?.ignore_target;

    if (requiresTarget && (!target || target.trim() === '')) {
      throw new Error('Target cannot be empty');
    }

    const trimmedTarget = requiresTarget ? target.trim() : '';
    const simpleCommandId = `${command}-${trimmedTarget}-${selectedAgent}`;
    
    // Real command ID will be received from backend
    let realCommandId = '';

    // Add to active commands set using simple ID
    setActiveCommands(prev => new Set(prev).add(simpleCommandId));

    // Add to history using simple ID initially
    const historyEntry: CommandHistory = {
      id: simpleCommandId,
      command,
      target: trimmedTarget,
      agent: selectedAgent,
      timestamp: Date.now(),
      ip_version: ipVersion
    };

    setCommandHistory(prev => [historyEntry, ...prev.filter(h => h.id !== simpleCommandId)]);

    const request = {
      type: 'execute_command',
      agent: selectedAgent,
      command,
      target: trimmedTarget,
      ip_version: ipVersion
    };

    return new Promise((resolve, reject) => {
      let latestOutput = ''; // Store only the latest output frame

      // No artificial timeout - let the command run as long as WebSocket connection is alive
      // The command will complete when the backend sends is_complete=true or connection is lost

      // Handle WebSocket connection loss
      const connectionLostHandler = () => {
        setActiveCommands(prev => {
          const newSet = new Set(prev);
          newSet.delete(simpleCommandId);
          return newSet;
        });
        if (realCommandId) {
          setStreamingOutputs(prev => {
            const newMap = new Map(prev);
            newMap.delete(realCommandId);
            return newMap;
          });
        }
        reject(new Error('Connection lost during command execution'));
      };

      // Listen for connection close events
      const originalOnClose = socketRef.current?.onclose;
      if (socketRef.current) {
        const currentSocket = socketRef.current;
        currentSocket.onclose = (event) => {
          connectionLostHandler();
          if (originalOnClose) {
            originalOnClose.call(currentSocket, event);
          }
        };
      }

      const messageHandler = (event: MessageEvent) => {
        try {
          const response = JSON.parse(event.data);

          // Handle streaming output chunks
          if (response.type === 'command_output' &&
            response.command === command &&
            response.agent === selectedAgent &&
            response.target === trimmedTarget &&
            !response.is_complete) {

            // Store latest output frame (replace mode)
            if (response.output) {
              latestOutput = response.output;
            }
            if (response.error) {
              latestOutput = response.error;
            }
            
            // Store real command ID from backend for stop functionality and output tracking
            if (response.command_id && !realCommandId) {
              realCommandId = response.command_id;
              setCommandIdMapping(prev => {
                const newMap = new Map(prev);
                newMap.set(simpleCommandId, response.command_id);
                return newMap;
              });
            }
          }

          // Handle streaming output completion
          else if (response.type === 'command_output' &&
            response.command === command &&
            response.agent === selectedAgent &&
            response.target === trimmedTarget &&
            response.is_complete) {

            socketRef.current?.removeEventListener('message', messageHandler);

            // Restore original onclose handler
            if (socketRef.current && originalOnClose) {
              socketRef.current.onclose = originalOnClose;
            }

            // Small delay to ensure streamingOutputs state is updated
            setTimeout(() => {
              // Get the final output from streaming outputs using real command ID
              let finalOutput = streamingOutputs.get(realCommandId) || latestOutput || '';
              
              // If command failed and there's an error message, use it as output
              if (!response.success && response.error && !finalOutput) {
                finalOutput = response.error;
              }

              const commandResponse: CommandResponse = {
                success: response.success,
                command: response.command,
                target: response.target,
                agent: response.agent,
                output: finalOutput,
                error: response.error,
                timestamp: Date.now(),
                stopped: response.stopped || false
              };

              // Update command history with final result
              setCommandHistory(prev => {
                const existingIndex = prev.findIndex(h => h.id === simpleCommandId);
                if (existingIndex >= 0) {
                  const updated = [...prev];
                  const finalResponse = {
                    ...commandResponse,
                    output: finalOutput,
                    stopped: response.stopped || false
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

              // Clean up active commands
              setActiveCommands(prev => {
                const newSet = new Set(prev);
                newSet.delete(simpleCommandId);
                return newSet;
              });

              // Clean up streaming output
              if (realCommandId) {
                setStreamingOutputs(prev => {
                  const newMap = new Map(prev);
                  newMap.delete(realCommandId);
                  return newMap;
                });
              }

              // Clean up command ID mapping
              setCommandIdMapping(prev => {
                const newMap = new Map(prev);
                newMap.delete(simpleCommandId);
                return newMap;
              });

              if (response.success && !response.stopped) {
                resolve({ response: commandResponse, realCommandId });
              } else if (response.stopped) {
                resolve({ response: commandResponse, realCommandId });
              } else {
                reject(new Error(response.error || 'Command execution failed'));
              }
            }, 50); // 50ms delay to ensure state update
          }

          // Handle legacy non-streaming response (fallback)
          else if (response.command === command &&
            response.agent === selectedAgent &&
            response.target === trimmedTarget &&
            !response.type) {

            socketRef.current?.removeEventListener('message', messageHandler);

            // Restore original onclose handler
            if (socketRef.current && originalOnClose) {
              socketRef.current.onclose = originalOnClose;
            }

            // If command failed and there's an error message, use it as output
            const finalOutput = !response.success && response.error && !response.output 
              ? response.error 
              : response.output;

            const commandResponse: CommandResponse = {
              success: response.success,
              command: response.command,
              target: response.target,
              agent: response.agent,
              output: finalOutput,
              error: response.error,
              timestamp: Date.now()
            };

            // Clean up
            setActiveCommands(prev => {
              const newSet = new Set(prev);
              newSet.delete(simpleCommandId);
              return newSet;
            });

            if (response.success) {
              resolve({ response: commandResponse, realCommandId: realCommandId || simpleCommandId });
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
  }, [selectedAgent, commands]);

  const clearHistory = useCallback(() => {
    setCommandHistory([]);
    setLocalStorage('yals_command_history', '[]');
  }, [setLocalStorage]);

  const clearStreamingOutput = useCallback((commandId: string) => {
    setStreamingOutputs(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });
  }, []);

  const clearAllStreamingOutputs = useCallback(() => {
    setStreamingOutputs(new Map());
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

    // Get the real backend command ID from mapping
    const realCommandId = commandIdMapping.get(commandId) || commandId;

    // Send stop command to backend with real command ID
    const stopRequest = {
      type: 'stop_command',
      command_id: realCommandId
    };

    socketRef.current.send(JSON.stringify(stopRequest));

    // Clean up mapping after stop
    setCommandIdMapping(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });

    // Note: Don't immediately clean up state, wait for backend stop confirmation
  }, [commandIdMapping]);

  // Write to Cookie when history updates (4KB limit, large history may be truncated)
  useEffect(() => {
    try {
      document.cookie = `yals_command_history=${encodeURIComponent(JSON.stringify(commandHistory))}`;
    } catch { }
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
      setCommands([]);
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
    clearAllStreamingOutputs,
    stopCommand
  };
};