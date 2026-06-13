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
  disabled?: boolean;
  onToggle: () => void;
}

const AgentItem: React.FC<AgentItemProps> = React.memo(({
  agent,
  isExpanded,
  isSelected,
  isOnline,
  disabled = false,
  onToggle
}) => {
  const StatusIcon = isOnline ? CheckCircle : XCircle;

  return (
    <div className={`agent-item-container ${isOnline ? 'online' : 'offline'} ${isSelected ? 'selected' : ''} ${disabled ? 'opacity-60 pointer-events-none' : ''}`}>
      <div
        className="agent-item-header"
        onClick={() => {
          if (!disabled) {
            onToggle();
          }
        }}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <StatusIcon className={`status-icon ${isOnline ? 'online' : 'offline'}`} />
          <div className="min-w-0 flex-1">
            <h3 className={`text-sm font-medium truncate ${
              isOnline ? 'u-text' : 'u-text-muted'
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
          <ChevronUp className="w-4 h-4 u-text-faint ml-2 flex-shrink-0" />
        ) : (
          <ChevronDown className="w-4 h-4 u-text-faint ml-2 flex-shrink-0" />
        )}
      </div>

      <AgentDetails agent={agent} isExpanded={isExpanded} />
    </div>
  );
});

interface AgentSelectorProps {
  groups: AgentGroupData;
  selectedAgent: string | null;
  disabled?: boolean;
  onSelectAgent: (agentName: string) => void;
}

export const AgentSelector: React.FC<AgentSelectorProps> = React.memo(({
  groups,
  selectedAgent,
  disabled = false,
  onSelectAgent
}) => {
  const [selectedGroup, setSelectedGroup] = useState('all');
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);

  const groupConfig = useMemo(() => {
    if (Array.isArray(groups)) {
      return groups
        .filter(group => Array.isArray(group.agents))
        .map(group => ({
          name: group.name,
          agents: group.agents.map(agent => agent.name)
        }));
    }

    return Object.entries(groups)
      .filter(([, agents]) => Array.isArray(agents))
      .map(([groupName, agents]) => ({
        name: groupName,
        agents: agents.map(agent => agent.name)
      }));
  }, [groups]);

  const filteredAgents = useMemo(() => {
    if (selectedGroup === 'all') {
      if (Array.isArray(groups)) {
        return groups.flatMap(group => group.agents || []);
      }
      return Object.values(groups).flat();
    }

    if (Array.isArray(groups)) {
      const selectedGroupData = groups.find(group => group.name === selectedGroup);
      return selectedGroupData?.agents || [];
    }

    return groups[selectedGroup] || [];
  }, [groups, selectedGroup]);

  const { onlineAgents, offlineAgents } = useMemo(() => ({
    onlineAgents: filteredAgents.filter(agent => agent.status === 1),
    offlineAgents: filteredAgents.filter(agent => agent.status !== 1)
  }), [filteredAgents]);

  const handleAgentToggle = useCallback((agent: Agent) => {
    if (disabled) {
      return;
    }
    onSelectAgent(agent.name);
    setExpandedAgent(expandedAgent === agent.name ? null : agent.name);
  }, [disabled, onSelectAgent, expandedAgent]);

  return (
    <div className="u-surface shadow-sm border u-border p-4 rounded-md">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 u-text-muted" />
          <h2 className="text-base font-semibold u-text">Servers</h2>
          <span className="text-xs u-text-muted">({onlineAgents.length} online)</span>
        </div>
        <select
          value={selectedGroup}
          onChange={(e) => setSelectedGroup(e.target.value)}
          className="command-select"
          disabled={disabled}
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
        <div className="text-center py-6 u-text-muted">
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
              disabled={disabled}
              onToggle={() => handleAgentToggle(agent)}
            />
          ))}

          {offlineAgents.length > 0 && (
            <>
              <div className="border-t u-border pt-2 mt-4">
                <h3 className="text-xs font-medium u-text-muted mb-2">Offline</h3>
              </div>
              {offlineAgents.map((agent) => (
                <AgentItem
                  key={agent.name}
                  agent={agent}
                  isExpanded={expandedAgent === agent.name}
                  isSelected={selectedAgent === agent.name}
                  isOnline={false}
                  disabled={disabled}
                  onToggle={() => {
                    if (!disabled) {
                      setExpandedAgent(expandedAgent === agent.name ? null : agent.name);
                    }
                  }}
                />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
});
