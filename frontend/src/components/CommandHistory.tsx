import React, { useState } from 'react';
import { History, Clock, Server, Target, CheckCircle, XCircle, Trash2, ChevronDown, ChevronRight, Copy } from 'lucide-react';
import { CommandHistory as CommandHistoryType } from '../types/yals';

interface CommandHistoryProps {
  history: CommandHistoryType[];
  activeCommands: Set<string>;
  onClearHistory: () => void;
}

export const CommandHistory: React.FC<CommandHistoryProps> = ({
  history,
  activeCommands,
  onClearHistory
}) => {
  const [expandedItems, setExpandedItems] = useState<Set<string>>(new Set());

  const toggleExpanded = (id: string) => {
    setExpandedItems(prev => {
      const newSet = new Set(prev);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return newSet;
    });
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      // 可以添加一个toast通知
    });
  };

  const formatTimestamp = (timestamp: number) => {
    return new Date(timestamp).toLocaleString('zh-CN');
  };

  const getCommandClassName = (command: string) => {
    switch (command) {
      case 'ping': return 'command-type-ping';
      case 'mtr': return 'command-type-mtr';
      case 'nexttrace': return 'command-type-nexttrace';
      default: return 'command-type-default';
    }
  };

  return (
    <div className="command-history-container">
      <div className="command-history-header">
        <div className="command-history-title">
          <History className="icon-small text-gray-600" />
          <h2 className="title-base">执行历史</h2>
          <span className="history-count">({history.length})</span>
        </div>
        
        {history.length > 0 && (
          <button
            onClick={onClearHistory}
            className="clear-history-btn"
          >
            <Trash2 className="icon-xs" />
            清空
          </button>
        )}
      </div>

      {history.length === 0 ? (
        <div className="empty-history-state">
          <History className="empty-history-icon" />
          <p className="empty-history-text">暂无历史</p>
          <p className="empty-history-subtext">执行诊断后显示结果</p>
        </div>
      ) : (
        <div className="history-list">
          {history.map((item) => {
            const isActive = activeCommands.has(item.id);
            const isExpanded = expandedItems.has(item.id);
            const hasResponse = !!item.response;
            const isSuccess = hasResponse && item.response?.success;

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
                        isSuccess ? (
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
                      <div className="meta-item">
                        <Target className="icon-xs" />
                        <span className="font-mono">{item.target}</span>
                      </div>
                      <div className="meta-item">
                        <Server className="icon-xs" />
                        <span>{item.agent}</span>
                      </div>
                      <div className="meta-item">
                        <Clock className="icon-xs" />
                        <span>{formatTimestamp(item.timestamp)}</span>
                      </div>
                    </div>
                  </div>

                  <div className="history-item-status-text">
                    {isActive && (
                      <span className="status-text running">执行中</span>
                    )}
                    {hasResponse && !isActive && (
                      <span className={`status-text ${isSuccess ? 'success' : 'error'}`}>
                        {isSuccess ? '完成' : '失败'}
                      </span>
                    )}
                  </div>
                </div>

                {isExpanded && hasResponse && (
                  <div className="history-item-content">
                    {item.response?.success ? (
                        <div>
                          <div className="result-header">
                            <h4 className="result-title">结果</h4>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                copyToClipboard(item.response?.output || '');
                              }}
                              className="copy-result-btn"
                            >
                              <Copy className="icon-xs" />
                              复制
                            </button>
                          </div>
                          <div className="result-output-container">
                            <div className="result-output">
                              {item.response.output || '无输出内容'}
                            </div>
                          </div>
                        </div>
                    ) : (
                      <div>
                        <h4 className="error-title">失败</h4>
                        <div className="error-message">
                          {item.response?.error || '未知错误'}
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
};