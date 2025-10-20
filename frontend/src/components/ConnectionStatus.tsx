import React from 'react';
import { Wifi, WifiOff, Loader2 } from 'lucide-react';

interface ConnectionStatusProps {
  isConnected: boolean;
  isConnecting: boolean;
  onConnect: () => void;
  onDisconnect: () => void;
}

export const ConnectionStatus: React.FC<ConnectionStatusProps> = ({
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
        {isConnecting ? '连接中...' : isConnected ? '已连接' : '未连接'}
      </span>
    </div>
  );
};