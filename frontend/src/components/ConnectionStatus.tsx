import React from 'react';
import { Wifi, WifiOff, Loader2 } from 'lucide-react';

interface ConnectionStatusProps {
  isConnected: boolean;
  isConnecting: boolean;
  onConnect: () => void;
  onDisconnect: () => void;
}

export const ConnectionStatus: React.FC<ConnectionStatusProps> = React.memo(({
  isConnected,
  isConnecting
}) => {
  return (
    <div className="connection-status">
      {isConnecting ? (
        <Loader2 className="connection-status-icon connecting animate-spin" />
      ) : isConnected ? (
        <Wifi className="connection-status-icon connected" />
      ) : (
        <WifiOff className="connection-status-icon disconnected" />
      )}
      <span className={`connection-status-text ${
        isConnected ? 'connected' : isConnecting ? 'connecting' : 'disconnected'
      }`}>
        {isConnecting ? 'Connecting...' : isConnected ? 'Connected' : 'Disconnected'}
      </span>
    </div>
  );
});