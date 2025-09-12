import React, { useState } from 'react';
import { History, Clock, Server, Target, CheckCircle, XCircle, Trash2, ChevronDown, ChevronRight, Copy } from 'lucide-react';
import { CommandHistory as CommandHistoryType } from '../types/yals';
import { Terminal as XTermComponent } from './Terminal';

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

  const getCommandColor = (command: string) => {
    switch (command) {
      case 'ping': return 'bg-green-100 text-green-800';
      case 'mtr': return 'bg-blue-100 text-blue-800';
      case 'nexttrace': return 'bg-purple-100 text-purple-800';
      default: return 'bg-gray-100 text-gray-800';
    }
  };

  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-4 flex flex-col flex-1">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <History className="w-4 h-4 text-gray-600" />
          <h2 className="text-base font-semibold text-gray-900">执行历史</h2>
          <span className="text-xs text-gray-500">({history.length})</span>
        </div>
        
        {history.length > 0 && (
          <button
            onClick={onClearHistory}
            className="flex items-center gap-1 px-2 py-1 text-xs text-red-600 hover:bg-red-50 rounded-md transition-colors"
          >
            <Trash2 className="w-3 h-3" />
            清空
          </button>
        )}
      </div>

      {history.length === 0 ? (
        <div className="text-center py-8 text-gray-500 flex-1 flex flex-col items-center justify-center">
          <History className="w-8 h-8 mb-2 text-gray-300" />
          <p className="text-sm">暂无历史</p>
          <p className="text-xs mt-0.5">执行诊断后显示结果</p>
        </div>
      ) : (
        <div className="space-y-2 flex-1">
          {history.map((item) => {
            const isActive = activeCommands.has(item.id);
            const isExpanded = expandedItems.has(item.id);
            const hasResponse = !!item.response;
            const isSuccess = hasResponse && item.response?.success;

            return (
              <div
                key={item.id}
                className={`border rounded-lg transition-all duration-200 ${
                  isActive ? 'border-blue-300 bg-blue-50' : 'border-gray-200 hover:border-gray-300'
                }`}
              >
                <div
                  className="p-3 cursor-pointer"
                  onClick={() => toggleExpanded(item.id)}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="flex items-center gap-1">
                        {isExpanded ? (
                          <ChevronDown className="w-3 h-3 text-gray-400" />
                        ) : (
                          <ChevronRight className="w-3 h-3 text-gray-400" />
                        )}
                        
                        {isActive ? (
                          <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></div>
                        ) : hasResponse ? (
                          isSuccess ? (
                            <CheckCircle className="w-3 h-3 text-green-500" />
                          ) : (
                            <XCircle className="w-3 h-3 text-red-500" />
                          )
                        ) : (
                          <div className="w-2 h-2 bg-gray-300 rounded-full"></div>
                        )}
                      </div>

                      <span className={`px-1.5 py-0.5 text-xs font-medium rounded ${getCommandColor(item.command)}`}>
                        {item.command.toUpperCase()}
                      </span>

                      <div className="flex items-center gap-3 text-xs text-gray-600">
                        <div className="flex items-center gap-1">
                          <Target className="w-2.5 h-2.5" />
                          <span className="font-mono">{item.target}</span>
                        </div>
                        <div className="flex items-center gap-1">
                          <Server className="w-2.5 h-2.5" />
                          <span>{item.agent}</span>
                        </div>
                        <div className="flex items-center gap-1">
                          <Clock className="w-2.5 h-2.5" />
                          <span>{formatTimestamp(item.timestamp)}</span>
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5">
                      {isActive && (
                        <span className="text-xs text-blue-600">执行中</span>
                      )}
                      {hasResponse && !isActive && (
                        <span className={`text-xs ${
                          isSuccess ? 'text-green-600' : 'text-red-600'
                        }`}>
                          {isSuccess ? '成功' : '失败'}
                        </span>
                      )}
                    </div>
                  </div>
                </div>

                {isExpanded && hasResponse && (
                  <div className="border-t border-gray-200 p-3 bg-gray-50">
                    {item.response?.success ? (
                        <div>
                          <div className="flex items-center justify-between mb-1">
                            <h4 className="text-xs font-medium text-gray-900">结果</h4>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                copyToClipboard(item.response?.output || '');
                              }}
                              className="flex items-center gap-0.5 px-1.5 py-0.5 text-xs text-gray-600 hover:bg-gray-200 rounded transition-colors"
                            >
                              <Copy className="w-2.5 h-2.5" />
                              复制
                            </button>
                          </div>
                          <div className="min-h-[100px]">
                            <XTermComponent output={item.response.output} />
                          </div>
                        </div>
                    ) : (
                      <div>
                        <h4 className="text-xs font-medium text-red-900 mb-1">失败</h4>
                        <div className="text-xs text-red-700 bg-red-50 p-2 rounded border border-red-200">
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