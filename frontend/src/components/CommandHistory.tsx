import React, { useState, useCallback } from 'react';
import { History, Clock, Server, Target, CheckCircle, XCircle, Trash2, ChevronDown, ChevronRight, Copy, TriangleAlert } from 'lucide-react';
import { CommandHistory as CommandHistoryType, CommandConfig } from '../types/yals';

interface CommandHistoryProps {
  history: CommandHistoryType[];
  activeCommands: Set<string>;
  onClearHistory: () => void;
  commands: CommandConfig[];
}

export const CommandHistory: React.FC<CommandHistoryProps> = React.memo(({
  history,
  activeCommands,
  onClearHistory,
  commands
}) => {
  const [expandedItems, setExpandedItems] = useState<Set<string>>(new Set());

  const toggleExpanded = useCallback((id: string) => {
    setExpandedItems(prev => {
      const newSet = new Set(prev);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return newSet;
    });
  }, []);

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      // Could add a toast notification here
    });
  }, []);

  const formatTimestamp = useCallback((timestamp: number) => {
    return new Date(timestamp).toLocaleString('zh-CN');
  }, []);

  const getCommandClassName = useCallback((command: string) => {
    switch (command) {
      case 'ping': return 'command-type-ping';
      case 'mtr': return 'command-type-mtr';
      case 'nexttrace': return 'command-type-nexttrace';
      default: return 'command-type-default';
    }
  }, []);

  return (
    <div className="command-history-container">
      <div className="command-history-header">
        <div className="command-history-title">
          <History className="icon-small text-gray-600" />
          <h2 className="title-base">Execution History</h2>
          <span className="history-count">({history.length})</span>
        </div>
        
        {history.length > 0 && (
          <button
            onClick={onClearHistory}
            className="clear-history-btn"
          >
            <Trash2 className="icon-xs" />
            Clear
          </button>
        )}
      </div>

      {history.length === 0 ? (
        <div className="empty-history-state">
          <History className="empty-history-icon" />
          <p className="empty-history-text">No History</p>
          <p className="empty-history-subtext">Results will appear after running diagnostics</p>
        </div>
      ) : (
        <div className="history-list">
          {history.map((item) => {
            const isActive = activeCommands.has(item.id);
            const isExpanded = expandedItems.has(item.id);
            const hasResponse = !!item.response;
            const isSuccess = hasResponse && (item.response?.success ?? false);

            return (
              <div
                key={item.id}
                className={`history-item ${isActive ? 'active' : ''}`}
              >
                <div
                  className="history-item-header"
                  onClick={() => toggleExpanded(item.id)}
                >
                  <div className="history-item-info">
                    <div className="history-item-status">
                      {isExpanded ? (
                        <ChevronDown className="icon-xs text-gray-400" />
                      ) : (
                        <ChevronRight className="icon-xs text-gray-400" />
                      )}
                      
                      {isActive ? (
                        <div className="status-indicator active"></div>
                      ) : hasResponse ? (
                        item.response?.stopped ? (
                          <TriangleAlert className="icon-xs text-yellow-500" />
                        ) : isSuccess ? (
                          <CheckCircle className="icon-xs text-green-500" />
                        ) : (
                          <XCircle className="icon-xs text-red-500" />
                        )
                      ) : (
                        <div className="status-indicator"></div>
                      )}
                    </div>

                    <span className={`command-type-badge ${getCommandClassName(item.command)}`}>
                      {item.command.toUpperCase()}
                    </span>

                    <div className="history-item-meta">
                      {(() => {
                        const commandConfig = commands.find(cmd => cmd.name === item.command);
                        const requiresTarget = !(commandConfig?.ignore_target ?? false);
                        
                        return (
                          <>
                            {requiresTarget && (
                              <div className="meta-item">
                                <Target className="icon-xs" />
                                <span className="font-mono">{item.target}</span>
                              </div>
                            )}
                            <div className="meta-item">
                              <Server className="icon-xs" />
                              <span>{item.agent}</span>
                            </div>
                            <div className="meta-item">
                              <Clock className="icon-xs" />
                              <span>{formatTimestamp(item.timestamp)}</span>
                            </div>
                          </>
                        );
                      })()}
                    </div>
                  </div>

                  <div className="history-item-status-text">
                    {isActive && (
                      <span className="status-text running">Running</span>
                    )}
                    {hasResponse && !isActive && (
                      <span className={`status-text ${item.response?.stopped ? 'stopped' : (isSuccess ? 'success' : 'error')}`}>
                        {item.response?.stopped ? 'Stopped' : (isSuccess ? 'Completed' : 'Failed')}
                      </span>
                    )}
                  </div>
                </div>

                {isExpanded && hasResponse && (
                  <div className="history-item-content">
                    {item.response?.success ? (
                        <div>
                          <div className="result-header">
                            <h4 className="result-title">Result</h4>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                copyToClipboard(item.response?.output || '');
                              }}
                              className="copy-result-btn"
                            >
                              <Copy className="icon-xs" />
                              Copy
                            </button>
                          </div>
                          <div className="result-output-container">
                            <div className="result-output">
                              {item.response.output || 'No output'}
                            </div>
                          </div>
                        </div>
                    ) : (
                      <div>
                        <h4 className="error-title">Failed</h4>
                        <div className="error-message">
                          {item.response?.error || 'Unknown error'}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
});