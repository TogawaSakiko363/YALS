/**
 * ANSI color code parser for terminal output
 * Converts ANSI escape sequences to HTML with CSS classes
 */

interface AnsiStyle {
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
  foreground?: string;
  background?: string;
}

interface ParsedSegment {
  text: string;
  style: AnsiStyle;
}

const ANSI_REGEX = /\x1b\[([0-9;]*?)m/g;

const ANSI_CODES: Record<number, (style: AnsiStyle) => void> = {
  0: (style) => {
    // Reset all
    style.bold = false;
    style.dim = false;
    style.italic = false;
    style.underline = false;
    style.foreground = undefined;
    style.background = undefined;
  },
  1: (style) => { style.bold = true; },
  2: (style) => { style.dim = true; },
  3: (style) => { style.italic = true; },
  4: (style) => { style.underline = true; },
  
  // Foreground colors (30-37)
  30: (style) => { style.foreground = 'black'; },
  31: (style) => { style.foreground = 'red'; },
  32: (style) => { style.foreground = 'green'; },
  33: (style) => { style.foreground = 'yellow'; },
  34: (style) => { style.foreground = 'blue'; },
  35: (style) => { style.foreground = 'magenta'; },
  36: (style) => { style.foreground = 'cyan'; },
  37: (style) => { style.foreground = 'white'; },
  
  // Bright foreground colors (90-97)
  90: (style) => { style.foreground = 'bright-black'; },
  91: (style) => { style.foreground = 'bright-red'; },
  92: (style) => { style.foreground = 'bright-green'; },
  93: (style) => { style.foreground = 'bright-yellow'; },
  94: (style) => { style.foreground = 'bright-blue'; },
  95: (style) => { style.foreground = 'bright-magenta'; },
  96: (style) => { style.foreground = 'bright-cyan'; },
  97: (style) => { style.foreground = 'bright-white'; },
  
  // Background colors (40-47)
  40: (style) => { style.background = 'black'; },
  41: (style) => { style.background = 'red'; },
  42: (style) => { style.background = 'green'; },
  43: (style) => { style.background = 'yellow'; },
  44: (style) => { style.background = 'blue'; },
  45: (style) => { style.background = 'magenta'; },
  46: (style) => { style.background = 'cyan'; },
  47: (style) => { style.background = 'white'; },
  
  // Bright background colors (100-107)
  100: (style) => { style.background = 'bright-black'; },
  101: (style) => { style.background = 'bright-red'; },
  102: (style) => { style.background = 'bright-green'; },
  103: (style) => { style.background = 'bright-yellow'; },
  104: (style) => { style.background = 'bright-blue'; },
  105: (style) => { style.background = 'bright-magenta'; },
  106: (style) => { style.background = 'bright-cyan'; },
  107: (style) => { style.background = 'bright-white'; },
};

export function parseAnsiText(text: string): ParsedSegment[] {
  const segments: ParsedSegment[] = [];
  let lastIndex = 0;
  let currentStyle: AnsiStyle = {};

  let match;
  while ((match = ANSI_REGEX.exec(text)) !== null) {
    // Add text before the ANSI code
    if (match.index > lastIndex) {
      segments.push({
        text: text.substring(lastIndex, match.index),
        style: { ...currentStyle }
      });
    }

    // Parse the ANSI code
    const codes = match[1].split(';').map(c => parseInt(c, 10)).filter(c => !isNaN(c));
    for (const code of codes) {
      if (code in ANSI_CODES) {
        ANSI_CODES[code](currentStyle);
      }
    }

    lastIndex = match.index + match[0].length;
  }

  // Add remaining text
  if (lastIndex < text.length) {
    segments.push({
      text: text.substring(lastIndex),
      style: { ...currentStyle }
    });
  }

  return segments;
}

export function getStyleClasses(style: AnsiStyle): string[] {
  const classes: string[] = [];

  if (style.bold) classes.push('ansi-bold');
  if (style.dim) classes.push('ansi-dim');
  if (style.italic) classes.push('ansi-italic');
  if (style.underline) classes.push('ansi-underline');
  if (style.foreground) classes.push(`ansi-${style.foreground}`);
  if (style.background) classes.push(`ansi-bg-${style.background}`);

  return classes;
}

export function renderAnsiToHtml(text: string): string {
  const segments = parseAnsiText(text);
  
  return segments.map(segment => {
    if (!segment.text) return '';
    
    const classes = getStyleClasses(segment.style);
    if (classes.length === 0) {
      return segment.text;
    }
    
    return `<span class="${classes.join(' ')}">${escapeHtml(segment.text)}</span>`;
  }).join('');
}

function escapeHtml(text: string): string {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;'
  };
  return text.replace(/[&<>"']/g, char => map[char]);
}
