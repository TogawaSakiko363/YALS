import { useState, useEffect, useCallback, useRef } from 'react';
import { Agent, AgentCommand, CommandResponse, CommandType, CommandHistory, AgentGroupData, CommandConfig, IPVersion } from '../types/yals';

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
  
  // Local storage utility functions
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
  const [abortControllers, setAbortControllers] = useState<Map<string, AbortController>>(new Map()); // Map command ID to AbortController
  const [sessionId, setSessionId] = useState<string | null>(null); // Store sessionID in React state

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

  // Fetch nodes data from /api/node
  const fetchNodesData = useCallback(async (sessionIdParam: string) => {
    try {
      const protocol = window.location.protocol;
      const response = await fetch(`${protocol}//${serverUrl}/api/node?session_id=${sessionIdParam}`, {
        method: 'GET',
        headers: {
          'Accept': 'application/json',
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to fetch nodes: ${response.status}`);
      }

      const data = await response.json();
      
      // Update app config
      setAppConfig({
        version: data.version,
        config: {
          agents_total: data.total_nodes.toString(),
          agents_online: data.online_nodes.toString(),
          agents_offline: data.offline_nodes.toString(),
        }
      });

      // Update groups
      setGroups(data.groups || []);

      // Extract agents from groups
      const allAgents: Agent[] = [];
      if (Array.isArray(data.groups)) {
        data.groups.forEach((group: any) => {
          if (Array.isArray(group.agents)) {
            allAgents.push(...group.agents);
          }
        });
      }
      setAgents(allAgents);

      // Auto-select first online agent if none selected
      if (!selectedAgent && allAgents.length > 0) {
        const onlineAgent = allAgents.find((agent: Agent) => agent.status === 1);
        if (onlineAgent) {
          setSelectedAgent(onlineAgent.name);
          // Set commands from agent
          const agentCommands = (onlineAgent as any).commands as AgentCommand[] | undefined;
          if (agentCommands && Array.isArray(agentCommands)) {
            const commandsList: CommandConfig[] = agentCommands.map((cmd: AgentCommand) => ({
              name: typeof cmd === 'string' ? cmd : cmd.name,
              description: typeof cmd === 'string' ? cmd : cmd.name,
              template: '',
              ignore_target: typeof cmd === 'string' ? false : (cmd.ignore_target || false)
            }));
            setCommands(commandsList);
            setLocalStorage('yals_commands', JSON.stringify(commandsList));
          }
        }
      }
    } catch (error) {
      console.error('YALS: Failed to fetch nodes data:', error);
    }
  }, [serverUrl, selectedAgent, setLocalStorage]);

  const connect = useCallback(() => {
    setIsConnecting(true);

    return new Promise<void>(async (resolve, reject) => {
      try {
        const protocol = window.location.protocol;
        
        // Check if sessionID already exists in state or sessionStorage
        let currentSessionId = sessionId;
        
        if (!currentSessionId) {
          currentSessionId = sessionStorage.getItem('yals_session_id') || null;
        }
        
        if (!currentSessionId) {
          const response = await fetch(`${protocol}//${serverUrl}/api/session`, {
            method: 'GET',
            headers: {
              'Accept': 'application/json',
            },
          });

          if (!response.ok) {
            throw new Error(`Failed to get session ID from server: ${response.status}`);
          }

          const sessionData = await response.json();
          currentSessionId = sessionData.session_id;
          
          if (!currentSessionId) {
            throw new Error('Server did not return a valid session ID');
          }

          // Store session ID in both sessionStorage and React state
          sessionStorage.setItem('yals_session_id', currentSessionId);
          setSessionId(currentSessionId);
          console.log('YALS: Received session ID from server:', currentSessionId);
        } else {
          // Update React state with existing sessionID
          setSessionId(currentSessionId);
        }
        
        // Fetch nodes data once on connect
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
        handleReconnect();
        reject(error);
      }
    });
  }, [serverUrl, fetchNodesData, handleReconnect, sessionId]);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    setIsConnected(false);
    setIsConnecting(false);
    reconnectAttemptsRef.current = maxReconnectAttempts;
  }, [maxReconnectAttempts]);

  const executeCommand = useCallback(async (command: CommandType, target: string, ipVersion: IPVersion = 'auto'): Promise<{ response: CommandResponse; realCommandId: string }> => {
    if (!isConnected) {
      throw new Error('Not connected to server');
    }

    if (!selectedAgent) {
      throw new Error('No node selected');
    }

    // Get sessionID with retry logic for high-latency environments
    let currentSessionId = sessionId;
    let retries = 0;
    const maxRetries = 5;
    const retryDelay = 100; // ms

    while (!currentSessionId && retries < maxRetries) {
      // Try React state first
      if (sessionId) {
        currentSessionId = sessionId;
        break;
      }
      
      // Try sessionStorage
      const stored = sessionStorage.getItem('yals_session_id');
      if (stored) {
        currentSessionId = stored;
        setSessionId(stored);
        break;
      }

      // Wait before retry
      await new Promise(resolve => setTimeout(resolve, retryDelay));
      retries++;
    }

    if (!currentSessionId) {
      throw new Error('No session ID available, please refresh the page.');
    }

    // Check if command requires target
    const commandConfig = commands.find(cmd => cmd.name === command);
    const requiresTarget = !commandConfig?.ignore_target;

    if (requiresTarget && (!target || target.trim() === '')) {
      throw new Error('Target cannot be empty');
    }

    const trimmedTarget = requiresTarget ? target.trim() : '';
    // Generate commandID with sessionID to match backend format
    const simpleCommandId = `${command}-${trimmedTarget}-${selectedAgent}-${currentSessionId}`;

    // Add to active commands
    setActiveCommands(prev => new Set(prev).add(simpleCommandId));

    // Add to history
    const historyEntry: CommandHistory = {
      id: simpleCommandId,
      command,
      target: trimmedTarget,
      agent: selectedAgent,
      timestamp: Date.now(),
      ip_version: ipVersion
    };

    setCommandHistory(prev => [historyEntry, ...prev.filter(h => h.id !== simpleCommandId)]);

    const protocol = window.location.protocol;
    const execUrl = `${protocol}//${serverUrl}/api/exec?session_id=${currentSessionId}`;

    return new Promise((resolve, reject) => {
      let accumulatedOutput = '';
      let backendCommandId = '';
      const abortController = new AbortController();

      // Store abort controller for this command
      setAbortControllers(prev => {
        const newMap = new Map(prev);
        newMap.set(simpleCommandId, abortController);
        return newMap;
      });

      // Use fetch to POST command, then listen to SSE
      fetch(execUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Accept': 'text/event-stream',
        },
        body: JSON.stringify({
          agent: selectedAgent,
          command,
          target: trimmedTarget,
          ip_version: ipVersion
        }),
        signal: abortController.signal
      }).then(response => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }

        if (!response.body) {
          throw new Error('Response body is null');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();

        const readStream = (): void => {
          reader.read().then(({ done, value }) => {
            if (done) {
              // Stream ended
              setActiveCommands(prev => {
                const newSet = new Set(prev);
                newSet.delete(simpleCommandId);
                return newSet;
              });
              // Clean up abort controller
              setAbortControllers(prev => {
                const newMap = new Map(prev);
                newMap.delete(simpleCommandId);
                return newMap;
              });
              return;
            }

            const chunk = decoder.decode(value, { stream: true });
            const lines = chunk.split('\n');

            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const data = line.substring(6);
                try {
                  const message = JSON.parse(data);

                  if (message.type === 'output') {
                    // Replace mode: directly use the output from backend
                    accumulatedOutput = message.output || '';
                    setStreamingOutputs(prev => {
                      const newMap = new Map(prev);
                      newMap.set(simpleCommandId, accumulatedOutput);
                      return newMap;
                    });
                  } else if (message.type === 'error') {
                    // Replace mode: directly use the error from backend
                    accumulatedOutput = message.error || '';
                    setStreamingOutputs(prev => {
                      const newMap = new Map(prev);
                      newMap.set(simpleCommandId, accumulatedOutput);
                      return newMap;
                    });
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

                    // Update history
                    setCommandHistory(prev => {
                      const existingIndex = prev.findIndex(h => h.id === simpleCommandId);
                      if (existingIndex >= 0) {
                        const updated = [...prev];
                        updated[existingIndex] = {
                          ...updated[existingIndex],
                          response: commandResponse
                        };
                        setLocalStorage('yals_command_history', JSON.stringify(updated.slice(0, 100)));
                        return updated;
                      }
                      return prev;
                    });

                    // Clean up active commands and abort controllers
                    setActiveCommands(prev => {
                      const newSet = new Set(prev);
                      newSet.delete(simpleCommandId);
                      return newSet;
                    });

                    setAbortControllers(prev => {
                      const newMap = new Map(prev);
                      newMap.delete(simpleCommandId);
                      return newMap;
                    });

                    // Keep streaming output for display (don't delete it)
                    // It will be shown until next command execution

                    if (message.success || message.stopped) {
                      resolve({ response: commandResponse, realCommandId: backendCommandId || simpleCommandId });
                    } else {
                      reject(new Error(message.error || 'Command execution failed'));
                    }
                    return;
                  }
                } catch (error) {
                  console.error('Failed to parse SSE message:', error);
                }
              }
            }

            readStream();
          }).catch(error => {
            console.error('Stream reading error:', error);
            setActiveCommands(prev => {
              const newSet = new Set(prev);
              newSet.delete(simpleCommandId);
              return newSet;
            });
            setAbortControllers(prev => {
              const newMap = new Map(prev);
              newMap.delete(simpleCommandId);
              return newMap;
            });
            if (error.name !== 'AbortError') {
              reject(error);
            }
          });
        };

        readStream();
      }).catch(error => {
        console.error('Command execution error:', error);
        setActiveCommands(prev => {
          const newSet = new Set(prev);
          newSet.delete(simpleCommandId);
          return newSet;
        });
        setAbortControllers(prev => {
          const newMap = new Map(prev);
          newMap.delete(simpleCommandId);
          return newMap;
        });
        if (error.name !== 'AbortError') {
          reject(error);
        }
      });
    });
  }, [isConnected, selectedAgent, commands, serverUrl, setLocalStorage, sessionId]);

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

  const stopCommand = useCallback(async (commandId: string) => {
    // Get sessionID with retry logic
    let currentSessionId = sessionId;
    let retries = 0;
    const maxRetries = 5;
    const retryDelay = 100; // ms

    while (!currentSessionId && retries < maxRetries) {
      if (sessionId) {
        currentSessionId = sessionId;
        break;
      }
      
      const stored = sessionStorage.getItem('yals_session_id');
      if (stored) {
        currentSessionId = stored;
        setSessionId(stored);
        break;
      }

      await new Promise(resolve => setTimeout(resolve, retryDelay));
      retries++;
    }

    if (!currentSessionId) {
      console.error('No session ID available');
      return;
    }

    // Get current streaming output before stopping
    const currentOutput = streamingOutputs.get(commandId) || '';

    // First, abort the fetch request
    const abortController = abortControllers.get(commandId);
    if (abortController) {
      abortController.abort();
    }

    // Then send stop request to backend
    const protocol = window.location.protocol;
    const stopUrl = `${protocol}//${serverUrl}/api/stop?session_id=${currentSessionId}`;

    try {
      const response = await fetch(stopUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          command_id: commandId
        })
      });

      if (!response.ok) {
        console.error('Failed to stop command:', response.status);
      } 
    } catch (error) {
      console.error('Error stopping command:', error);
    }

    // Clean up local state FIRST (to update button state immediately)
    setActiveCommands(prev => {
      const newSet = new Set(prev);
      newSet.delete(commandId);
      return newSet;
    });

    setAbortControllers(prev => {
      const newMap = new Map(prev);
      newMap.delete(commandId);
      return newMap;
    });

    // Update command history with stopped status
    setCommandHistory(prev => {
      const existingIndex = prev.findIndex(h => h.id === commandId);
      if (existingIndex >= 0) {
        const updated = [...prev];
        updated[existingIndex] = {
          ...updated[existingIndex],
          response: {
            success: false,
            command: updated[existingIndex].command,
            target: updated[existingIndex].target,
            agent: updated[existingIndex].agent,
            output: currentOutput + '\n\n*** Command Stopped ***',
            error: 'Command stopped by user',
            timestamp: Date.now(),
            stopped: true
          }
        };
        setLocalStorage('yals_command_history', JSON.stringify(updated.slice(0, 100)));
        return updated;
      }
      return prev;
    });

    // Clean up streaming outputs LAST (after history is updated)
    // Don't clean immediately to allow App.tsx to read it
    setTimeout(() => {
      setStreamingOutputs(prev => {
        const newMap = new Map(prev);
        newMap.delete(commandId);
        return newMap;
      });
    }, 100);
  }, [abortControllers, streamingOutputs, serverUrl, setLocalStorage, sessionId]);

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
      // Get commands from selected agent
      const agent = agents.find(a => a.name === agentName);
      const agentCommands = agent ? (agent as any).commands as AgentCommand[] | undefined : undefined;
      if (agent && agentCommands && Array.isArray(agentCommands)) {
        const commandsList: CommandConfig[] = agentCommands.map((cmd: AgentCommand) => ({
          name: typeof cmd === 'string' ? cmd : cmd.name,
          description: typeof cmd === 'string' ? cmd : cmd.name,
          template: '',
          ignore_target: typeof cmd === 'string' ? false : (cmd.ignore_target || false)
        }));
        setCommands(commandsList);
        setLocalStorage('yals_commands', JSON.stringify(commandsList));
      }
    } else {
      setCommands([]);
    }
  }, [agents, setLocalStorage]);

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
