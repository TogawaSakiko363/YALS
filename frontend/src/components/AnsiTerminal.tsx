import React, { useMemo } from 'react';
import { parseAnsiText, getStyleClasses } from '../utils/ansiParser';

interface AnsiTerminalProps {
  content: string;
  className?: string;
}

export const AnsiTerminal: React.FC<AnsiTerminalProps> = React.memo(({ content, className = '' }) => {
  const segments = useMemo(() => parseAnsiText(content), [content]);

  return (
    <div className={className}>
      {segments.map((segment, index) => {
        if (!segment.text) return null;

        const classes = getStyleClasses(segment.style);
        if (classes.length === 0) {
          return <span key={index}>{segment.text}</span>;
        }

        return (
          <span key={index} className={classes.join(' ')}>
            {segment.text}
          </span>
        );
      })}
    </div>
  );
});

AnsiTerminal.displayName = 'AnsiTerminal';
