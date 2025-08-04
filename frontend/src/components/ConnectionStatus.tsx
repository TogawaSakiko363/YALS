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
    <div className="flex items-center gap-2">
      {isConnecting ? (
        <Loader2 className="w-5 h-5 text-blue-500 animate-spin" />
      ) : isConnected ? (
        <Wifi className="w-5 h-5 text-green-500" />
      ) : (
        <WifiOff className="w-5 h-5 text-red-500" />
      )}
      <span className={`text-sm font-medium ${
        isConnected ? 'text-green-600' : isConnecting ? 'text-blue-600' : 'text-red-600'
      }`}>
        {isConnecting ? '连接中...' : isConnected ? '已连接' : '未连接'}
      </span>
    </div>
  );
};