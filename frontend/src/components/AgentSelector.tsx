import React, { useState } from 'react';
import { Server, CheckCircle, XCircle, ChevronDown, ChevronUp } from 'lucide-react';
import { AgentGroupData, Agent } from '../types/yals';

interface AgentDetailsProps {
  agent: Agent;
  isExpanded: boolean;
}

const AgentDetails: React.FC<AgentDetailsProps> = ({ agent, isExpanded }) => {
  if (!isExpanded || !agent.details) return null;

  return (
    <div className="px-3 pb-3 pt-1 border-t border-gray-100">
      <p className="text-xs text-gray-500">
        <span className="font-medium">测试IP:</span> {agent.details.test_ip}
      </p>
      <p className="text-xs text-gray-500 mt-1">
        {agent.details.description}
      </p>
    </div>
  );
};

interface AgentItemProps {
  agent: Agent;
  isExpanded: boolean;
  isSelected: boolean;
  isOnline: boolean;
  onToggle: () => void;
  onSelect: () => void;
}

const AgentItem: React.FC<AgentItemProps> = ({ 
  agent, 
  isExpanded, 
  isSelected, 
  isOnline, 
  onToggle
}) => {
  const StatusIcon = isOnline ? CheckCircle : XCircle;
  const statusColor = isOnline ? 'text-green-500' : 'text-gray-400';
  const containerClass = isOnline 
    ? `rounded-lg border transition-all duration-200 ${
        isSelected
          ? 'border-blue-500 bg-blue-50 shadow-sm'
          : 'border-gray-200 hover:border-gray-300 hover:shadow-sm'
      }`
    : 'rounded-lg border border-gray-200 bg-gray-50 opacity-60';

  return (
    <div className={containerClass}>
      <div
        className="flex items-center justify-between p-3 cursor-pointer"
        onClick={onToggle}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <StatusIcon className={`w-4 h-4 ${statusColor} flex-shrink-0`} />
          <div className="min-w-0 flex-1">
            <h3 className={`text-sm font-medium truncate ${
              isOnline ? 'text-gray-900' : 'text-gray-600'
            }`}>
              {agent.name}
            </h3>
            {agent.details && (
              <p className="text-xs text-gray-500">
                {agent.details.location} - {agent.details.datacenter}
              </p>
            )}
          </div>
        </div>
        {isExpanded ? (
          <ChevronUp className="w-4 h-4 text-gray-400 ml-2 flex-shrink-0" />
        ) : (
          <ChevronDown className="w-4 h-4 text-gray-400 ml-2 flex-shrink-0" />
        )}
      </div>
      
      <AgentDetails agent={agent} isExpanded={isExpanded} />
    </div>
  );
};

interface AgentSelectorProps {
  groups: AgentGroupData;
  selectedAgent: string | null;
  onSelectAgent: (agentName: string) => void;
}

export const AgentSelector: React.FC<AgentSelectorProps> = ({
  groups,
  selectedAgent,
  onSelectAgent
}) => {
  const [selectedGroup, setSelectedGroup] = useState('all');
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);

  // 从后端groups数据中提取分组配置（支持有序数组格式）
  const groupConfig = React.useMemo(() => {
    if (Array.isArray(groups)) {
      // 新格式：有序数组
      return groups
        .filter(group => Array.isArray(group.agents))
        .map(group => ({
          name: group.name,
          agents: group.agents.map(agent => agent.name)
        }));
    } else {
      // 旧格式：对象
      return Object.entries(groups)
        .filter(([, agents]) => Array.isArray(agents))
        .map(([groupName, agents]) => ({
          name: groupName,
          agents: agents.map(agent => agent.name)
        }));
    }
  }, [groups]);

  // 根据选择的分组过滤agents
  const filteredAgents = React.useMemo(() => {
    if (selectedGroup === 'all') {
      if (Array.isArray(groups)) {
        return groups.flatMap(group => group.agents || []);
      } else {
        return Object.values(groups).flat();
      }
    }
    
    if (Array.isArray(groups)) {
      const selectedGroupData = groups.find(group => group.name === selectedGroup);
      return selectedGroupData?.agents || [];
    } else {
      return groups[selectedGroup] || [];
    }
  }, [groups, selectedGroup]);

  const { onlineAgents, offlineAgents } = React.useMemo(() => ({
    onlineAgents: filteredAgents.filter(agent => agent.status === 1),
    offlineAgents: filteredAgents.filter(agent => agent.status !== 1)
  }), [filteredAgents]);

  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 text-gray-600" />
          <h2 className="text-base font-semibold text-gray-900">节点列表</h2>
          <span className="text-xs text-gray-500">({onlineAgents.length} 在线)</span>
        </div>
        <select
          value={selectedGroup}
          onChange={(e) => setSelectedGroup(e.target.value)}
          className="px-3 py-1.5 text-sm bg-white border border-gray-200 rounded-lg shadow-sm 
                     hover:border-gray-300 hover:shadow-md focus:outline-none focus:ring-2 
                     focus:ring-blue-500 focus:border-transparent transition-all duration-200 
                     cursor-pointer appearance-none bg-no-repeat bg-right pr-8
                     font-medium text-gray-700"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 20 20'%3e%3cpath stroke='%239ca3af' stroke-linecap='round' stroke-linejoin='round' stroke-width='1.5' d='M6 8l4 4 4-4'/%3e%3c/svg%3e")`,
            backgroundPosition: 'right 0.5rem center',
            backgroundSize: '1.25em 1.25em'
          }}
        >
          <option value="all" className="font-medium">全部节点</option>
          {groupConfig.map(group => (
            <option key={group.name} value={group.name} className="font-medium">
              {group.name} ({group.agents.length})
            </option>
           ))}
        </select>
      </div>

      {filteredAgents.length === 0 ? (
        <div className="text-center py-6 text-gray-500">
          <Server className="w-10 h-10 mx-auto mb-2 text-gray-300" />
          <p className="text-sm">暂无节点</p>
        </div>
      ) : (
        <div className="space-y-2">
          {onlineAgents.map((agent) => (
            <AgentItem
              key={agent.name}
              agent={agent}
              isExpanded={expandedAgent === agent.name}
              isSelected={selectedAgent === agent.name}
              isOnline={true}
              onToggle={() => {
                onSelectAgent(agent.name);
                setExpandedAgent(expandedAgent === agent.name ? null : agent.name);
              }}
              onSelect={() => onSelectAgent(agent.name)}
            />
          ))}

          {offlineAgents.length > 0 && (
            <>
              <div className="border-t border-gray-200 pt-2 mt-4">
                <h3 className="text-xs font-medium text-gray-500 mb-2">离线</h3>
              </div>
              {offlineAgents.map((agent) => (
                <AgentItem
                  key={agent.name}
                  agent={agent}
                  isExpanded={expandedAgent === agent.name}
                  isSelected={selectedAgent === agent.name}
                  isOnline={false}
                  onToggle={() => setExpandedAgent(expandedAgent === agent.name ? null : agent.name)}
                  onSelect={() => onSelectAgent(agent.name)}
                />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
};