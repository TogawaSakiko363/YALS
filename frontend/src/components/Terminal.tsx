import React, { useRef, useEffect } from 'react';
import 'xterm/css/xterm.css';
import { Terminal as XTerm } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

// 添加CSS样式来隐藏滚动条
const terminalStyles = `
  .terminal-container .xterm-viewport {
    scrollbar-width: none; /* Firefox */
    -ms-overflow-style: none; /* IE and Edge */
  }
  
  .terminal-container .xterm-viewport::-webkit-scrollbar {
    display: none; /* Chrome, Safari, Opera */
  }
  
  .terminal-container .xterm-screen {
    scrollbar-width: none;
    -ms-overflow-style: none;
  }
  
  .terminal-container .xterm-screen::-webkit-scrollbar {
    display: none;
  }
`;

// 注入样式
if (typeof document !== 'undefined') {
  const styleElement = document.createElement('style');
  styleElement.textContent = terminalStyles;
  document.head.appendChild(styleElement);
}

interface TerminalProps {
  output: string | null | undefined;
  streamingOutput?: string;
  isStreaming?: boolean;
}

export const Terminal: React.FC<TerminalProps> = ({ output, streamingOutput, isStreaming }) => {
  const terminalRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const lastOutputRef = useRef<string>('');

  useEffect(() => {
    // 初始化终端
    if (terminalRef.current && !termRef.current) {
      const fitAddon = new FitAddon();
      fitAddonRef.current = fitAddon;
      
      const term = new XTerm({
        cursorBlink: true,
        scrollback: 1000,
        fontSize: 14,
        fontFamily: 'Monaco, Menlo, "Ubuntu Mono", Consolas, "SF Mono", monospace',
        theme: {
          background: '#1F2937',
          foreground: '#F9FAFB',
          cursor: '#60A5FA',
          cursorAccent: '#1F2937',
          selectionBackground: '#374151',
          black: '#1F2937',
          red: '#EF4444',
          green: '#10B981',
          yellow: '#F59E0B',
          blue: '#3B82F6',
          magenta: '#8B5CF6',
          cyan: '#06B6D4',
          white: '#F9FAFB',
          brightBlack: '#6B7280',
          brightRed: '#F87171',
          brightGreen: '#34D399',
          brightYellow: '#FBBF24',
          brightBlue: '#60A5FA',
          brightMagenta: '#A78BFA',
          brightCyan: '#22D3EE',
          brightWhite: '#FFFFFF'
        }
      });

      term.loadAddon(fitAddon);
      term.open(terminalRef.current);
      termRef.current = term;

      // 初始提示文本由第二个useEffect根据output状态统一处理，避免重复显示
    }

    // 适应容器大小
    const handleResize = () => {
      if (termRef.current && fitAddonRef.current && terminalRef.current) {
        fitAddonRef.current.fit();
      }
    };

    window.addEventListener('resize', handleResize);
    handleResize(); // 初始调整

    return () => {
      window.removeEventListener('resize', handleResize);
      // 清理终端实例
      if (termRef.current) {
        termRef.current.dispose();
        termRef.current = null;
      }
    };
  }, []);

  // 更新输出内容
  useEffect(() => {
    if (termRef.current) {
      // 优先显示流式输出，如果没有流式输出则显示最终输出
      let displayOutput: string | null | undefined;
      if (isStreaming && streamingOutput !== undefined && streamingOutput !== '') {
        displayOutput = streamingOutput;
      } else if (output !== null && output !== undefined && output !== '') {
        displayOutput = output;
      } else if (streamingOutput !== undefined && streamingOutput !== '') {
        // 命令完成但没有最终输出时，显示流式输出的内容
        displayOutput = streamingOutput;
      } else {
        displayOutput = null;
      }
      
      // 只有当输出内容发生变化时才更新终端
      const currentOutput = displayOutput || '';
      if (currentOutput !== lastOutputRef.current) {
        lastOutputRef.current = currentOutput;
        
        // 清空终端
        termRef.current.clear();
        
        if (displayOutput === null || displayOutput === undefined) {
          termRef.current.writeln('请在上方选择命令类型和目标地址，然后点击"执行"开始测试');
        } else if (displayOutput && displayOutput.length > 0) {
          // 将输出按行写入终端
          const lines = displayOutput.split('\n');
          lines.forEach(line => {
            termRef.current?.writeln(line);
          });
          
          // 移除光标字符，不再显示"█"
        } else {
          // 只有在确实没有任何输出时才显示这个消息
          if (!isStreaming && (output === '' || (output === null && streamingOutput === ''))) {
            termRef.current.writeln('命令执行完成，无输出内容');
          }
        }
      }
    }
  }, [output, streamingOutput, isStreaming]);

  // 初始化时显示默认提示
  useEffect(() => {
    if (termRef.current && output === null && !streamingOutput && !isStreaming) {
      termRef.current.clear();
      termRef.current.writeln('请在上方选择命令类型和目标地址，然后点击"执行"开始测试');
    }
  }, [termRef.current]);

  return (
    <div 
      ref={terminalRef}
      className="bg-transparent text-white p-0 text-sm min-h-[300px] flex-1 overflow-hidden terminal-container"
      style={{ height: 'auto' }}
    />
  );
}