import React, { useState, useMemo, useCallback } from 'react';
import { Server, CheckCircle, XCircle, ChevronDown, ChevronUp } from 'lucide-react';
import { AgentGroupData, Agent } from '../types/yals';

interface AgentDetailsProps {
  agent: Agent;
  isExpanded: boolean;
}

const AgentDetails: React.FC<AgentDetailsProps> = React.memo(({ agent, isExpanded }) => {
  if (!isExpanded || !agent.details) return null;

  return (
    <div className="agent-details">
      <p className="agent-details-text">
        <span className="font-medium">Test IP:</span> {agent.details.test_ip}
      </p>
      <p className="agent-details-text">
        {agent.details.description}
      </p>
    </div>
  );
});

interface AgentItemProps {
  agent: Agent;
  isExpanded: boolean;
  isSelected: boolean;
  isOnline: boolean;
  onToggle: () => void;
  onSelect: () => void;
}

const AgentItem: React.FC<AgentItemProps> = React.memo(({ 
  agent, 
  isExpanded, 
  isSelected, 
  isOnline, 
  onToggle
}) => {
  const StatusIcon = isOnline ? CheckCircle : XCircle;

  return (
    <div className={`agent-item-container ${isOnline ? 'online' : 'offline'} ${isSelected ? 'selected' : ''}`}>
      <div
        className="agent-item-header"
        onClick={onToggle}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <StatusIcon className={`status-icon ${isOnline ? 'online' : 'offline'}`} />
          <div className="min-w-0 flex-1">
            <h3 className={`text-sm font-medium truncate ${
              isOnline ? 'text-gray-900' : 'text-gray-600'
            }`}>
              {agent.name}
            </h3>
            {agent.details && (
              <p className="agent-details-text">
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
});

interface AgentSelectorProps {
  groups: AgentGroupData;
  selectedAgent: string | null;
  onSelectAgent: (agentName: string) => void;
}

export const AgentSelector: React.FC<AgentSelectorProps> = React.memo(({
  groups,
  selectedAgent,
  onSelectAgent
}) => {
  const [selectedGroup, setSelectedGroup] = useState('all');
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);

  // Extract group configuration from backend groups data (supports ordered array format)
  const groupConfig = useMemo(() => {
    if (Array.isArray(groups)) {
      // New format: ordered array
      return groups
        .filter(group => Array.isArray(group.agents))
        .map(group => ({
          name: group.name,
          agents: group.agents.map(agent => agent.name)
        }));
    } else {
      // Old format: object
      return Object.entries(groups)
        .filter(([, agents]) => Array.isArray(agents))
        .map(([groupName, agents]) => ({
          name: groupName,
          agents: agents.map(agent => agent.name)
        }));
    }
  }, [groups]);

  // Filter agents based on selected group
  const filteredAgents = useMemo(() => {
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

  const { onlineAgents, offlineAgents } = useMemo(() => ({
    onlineAgents: filteredAgents.filter(agent => agent.status === 1),
    offlineAgents: filteredAgents.filter(agent => agent.status !== 1)
  }), [filteredAgents]);

  const handleAgentToggle = useCallback((agent: Agent) => {
    onSelectAgent(agent.name);
    setExpandedAgent(expandedAgent === agent.name ? null : agent.name);
  }, [onSelectAgent, expandedAgent]);

  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 text-gray-600" />
          <h2 className="text-base font-semibold text-gray-900">Node List</h2>
          <span className="text-xs text-gray-500">({onlineAgents.length} online)</span>
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
          <option value="all" className="font-medium">All Nodes</option>
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
          <p className="text-sm">No nodes available</p>
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
              onToggle={() => handleAgentToggle(agent)}
              onSelect={() => onSelectAgent(agent.name)}
            />
          ))}

          {offlineAgents.length > 0 && (
            <>
              <div className="border-t border-gray-200 pt-2 mt-4">
                <h3 className="text-xs font-medium text-gray-500 mb-2">Offline</h3>
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
});