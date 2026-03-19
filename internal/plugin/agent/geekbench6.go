package agent

import (
	"YALS/internal/plugin"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// GeekBench6Plugin implements the GeekBench 6 CPU benchmark plugin
type GeekBench6Plugin struct {
	mutex         sync.RWMutex
	lastResult    *GeekBench6Result
	isRunning     bool
	animationStop chan bool
}

// GeekBench6Result represents the result of a GeekBench 6 benchmark
type GeekBench6Result struct {
	Timestamp          time.Time
	CPUName            string
	BaseFrequency      string
	SingleCoreScore    string
	IntegerScore       string
	FloatingPointScore string
}

// Global instance to maintain state across requests
var geekbench6Instance = &GeekBench6Plugin{}

// GetName returns the plugin name
func (p *GeekBench6Plugin) GetName() string {
	return "geekbench6"
}

// GetDescription returns the plugin description
func (p *GeekBench6Plugin) GetDescription() string {
	return "GeekBench 6 CPU benchmark"
}

// GetIgnoreTarget implements PluginWithConfig interface
func (p *GeekBench6Plugin) GetIgnoreTarget() bool {
	return true
}

// GetMaximumQueue implements PluginWithConfig interface
func (p *GeekBench6Plugin) GetMaximumQueue() int {
	return 1
}

// CheckQueueLimit implements PluginWithQueueControl interface
func (p *GeekBench6Plugin) CheckQueueLimit() (bool, string) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.isRunning {
		message := "This command has reached its execution limit. Please try again later.\n"
		if p.lastResult != nil {
			message += p.formatLastResult()
		}
		return false, message
	}

	return true, ""
}

// stopAnimation stops the loading animation
func (p *GeekBench6Plugin) stopAnimation() {
	if p.animationStop != nil {
		select {
		case p.animationStop <- true:
		default:
		}
		p.animationStop = nil
	}
}

// Execute runs the GeekBench 6 command and returns formatted output
func (p *GeekBench6Plugin) Execute(target string) (string, error) {
	return p.executeInternal(context.Background(), nil, nil)
}

// ExecuteStreaming runs the GeekBench 6 command with streaming output
func (p *GeekBench6Plugin) ExecuteStreaming(target string, callback plugin.StreamingCallback) error {
	_, err := p.executeInternal(context.Background(), callback, nil)
	return err
}

// ExecuteStreamingWithID runs the GeekBench 6 command with command ID for stop functionality
func (p *GeekBench6Plugin) ExecuteStreamingWithID(target, commandID string, callback plugin.StreamingCallback) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := p.executeInternal(ctx, callback, &commandID)
	return err
}

// executeInternal handles the core execution logic
func (p *GeekBench6Plugin) executeInternal(ctx context.Context, callback plugin.StreamingCallback, commandID *string) (string, error) {
	// Check if already running (queue limit: 1)
	p.mutex.Lock()
	if p.isRunning {
		output := "This command has reached its execution limit. Please try again later.\n"
		if p.lastResult != nil {
			output += p.formatLastResult()
		}
		p.mutex.Unlock()

		if callback != nil {
			callback(output, false, true)
			return "", nil
		}
		return output, nil
	}

	p.isRunning = true
	lastResult := p.lastResult
	p.mutex.Unlock()

	defer func() {
		p.mutex.Lock()
		p.isRunning = false
		p.mutex.Unlock()
	}()

	commandPath := "/opt/geekbench6/geekbench6"

	if strings.Contains(commandPath, "..") || !strings.HasPrefix(commandPath, "/") {
		err := fmt.Errorf("invalid command path")
		if callback != nil {
			callback(err.Error(), true, true)
		}
		return "", err
	}

	if !plugin.IsCommandAvailable(commandPath) {
		err := fmt.Errorf("geekbench6 not found at %s", commandPath)
		if callback != nil {
			callback(err.Error(), true, true)
		}
		return "", err
	}

	if callback != nil {
		// Create a channel to stop the animation
		stopChan := make(chan bool, 1)

		// Start animation goroutine
		go func() {
			frames := []string{"|", "/", "-", "\\"}
			frameIndex := 0
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-stopChan:
					return
				case <-ticker.C:
					message := fmt.Sprintf("We are processing your request, please wait for about 3 minutes %s\n", frames[frameIndex])
					if lastResult != nil {
						message += p.formatLastResult()
					}
					callback(message, false, false)
					frameIndex = (frameIndex + 1) % len(frames)
				}
			}
		}()

		// Send initial message
		initialMessage := "We are processing your request, please wait for about 3 minutes...\n"
		if lastResult != nil {
			initialMessage += p.formatLastResult()
		}
		callback(initialMessage, false, false)

		// Store stopChan for later use (will be closed when command completes)
		p.animationStop = stopChan
	}

	cmd := exec.CommandContext(ctx, commandPath, "--single-core", "--no-upload")

	if commandID != nil {
		manager := plugin.GetManager()
		manager.RegisterActiveCommand(*commandID, cmd)
		defer manager.UnregisterActiveCommand(*commandID)
	}

	if callback != nil {
		return p.executeStreaming(ctx, cmd, callback)
	}
	return p.executeNonStreaming(cmd)
}

// executeNonStreaming handles non-streaming execution
func (p *GeekBench6Plugin) executeNonStreaming(cmd *exec.Cmd) (string, error) {
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("geekbench6 execution failed: %v", err)
	}

	result := p.parseGeekBench6Output(string(output))
	if result != nil {
		p.mutex.Lock()
		p.lastResult = result
		p.mutex.Unlock()
		return p.formatResult(result), nil
	}

	return "GeekBench 6 execution completed but failed to parse results", nil
}

// executeStreaming handles streaming execution
func (p *GeekBench6Plugin) executeStreaming(ctx context.Context, cmd *exec.Cmd, callback plugin.StreamingCallback) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		callback(fmt.Sprintf("Failed to create stdout pipe: %v", err), true, true)
		return "", err
	}

	if err := cmd.Start(); err != nil {
		callback(fmt.Sprintf("Failed to start GeekBench 6: %v", err), true, true)
		return "", err
	}

	var outputBuffer bytes.Buffer
	scanner := bufio.NewScanner(stdout)

	// Pre-allocate buffer for better performance
	outputBuffer.Grow(4096)

	go func() {
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Text()
				outputBuffer.WriteString(line)
				outputBuffer.WriteByte('\n')

				// Send progress updates for key lines
				if strings.Contains(line, "Running") ||
					strings.Contains(line, "Benchmark") ||
					strings.Contains(line, "Score") {
					callback(fmt.Sprintf("Progress: %s", line), false, false)
				}
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		p.stopAnimation()
		callback("GeekBench 6 execution cancelled", false, true)
		return "", nil
	case err := <-done:
		p.stopAnimation()
		if err != nil {
			callback(fmt.Sprintf("GeekBench 6 execution failed: %v", err), true, true)
			return "", err
		}

		result := p.parseGeekBench6Output(outputBuffer.String())

		p.mutex.Lock()
		if result != nil {
			p.lastResult = result
		}
		p.mutex.Unlock()

		completionMessage := "Your request has completed, thank you for using our service.\n"
		if result != nil {
			completionMessage += p.formatLatestResult(result)
		} else {
			completionMessage += "GeekBench 6 execution completed but failed to parse results"
		}

		callback(completionMessage, false, true)
		return "", nil
	}
}

// Pre-compiled regular expressions for better performance
var (
	nameRegex       = regexp.MustCompile(`Name\s+(.+)`)
	freqRegex       = regexp.MustCompile(`Base Frequency\s+(.+)`)
	singleCoreRegex = regexp.MustCompile(`Single-Core Score\s+(\d+)`)
	integerRegex    = regexp.MustCompile(`Integer Score\s+(\d+)`)
	floatingRegex   = regexp.MustCompile(`Floating Point Score\s+(\d+)`)
)

// parseGeekBench6Output parses GeekBench 6 output and extracts relevant information
func (p *GeekBench6Plugin) parseGeekBench6Output(output string) *GeekBench6Result {
	result := &GeekBench6Result{
		Timestamp: time.Now(),
	}

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if match := nameRegex.FindStringSubmatch(line); match != nil {
			result.CPUName = strings.TrimSpace(match[1])
		} else if match := freqRegex.FindStringSubmatch(line); match != nil {
			result.BaseFrequency = strings.TrimSpace(match[1])
		} else if match := singleCoreRegex.FindStringSubmatch(line); match != nil {
			result.SingleCoreScore = strings.TrimSpace(match[1])
		} else if match := integerRegex.FindStringSubmatch(line); match != nil {
			result.IntegerScore = strings.TrimSpace(match[1])
		} else if match := floatingRegex.FindStringSubmatch(line); match != nil {
			result.FloatingPointScore = strings.TrimSpace(match[1])
		}
	}

	if result.CPUName == "" && result.SingleCoreScore == "" {
		return nil
	}

	return result
}

// formatResult formats a GeekBench 6 result for display
func (p *GeekBench6Plugin) formatResult(result *GeekBench6Result) string {
	var output strings.Builder

	if result.CPUName != "" {
		output.WriteString(fmt.Sprintf("Name                          %s\n", result.CPUName))
	}
	if result.BaseFrequency != "" {
		output.WriteString(fmt.Sprintf("Base Frequency                %s\n", result.BaseFrequency))
	}

	output.WriteString("Benchmark Summary\n")

	if result.SingleCoreScore != "" {
		output.WriteString(fmt.Sprintf("Single-Core Score             %s\n", result.SingleCoreScore))
	}
	if result.IntegerScore != "" {
		output.WriteString(fmt.Sprintf("Integer Score                 %s\n", result.IntegerScore))
	}
	if result.FloatingPointScore != "" {
		output.WriteString(fmt.Sprintf("Floating Point Score          %s\n", result.FloatingPointScore))
	}

	return output.String()
}

// formatLastResult formats the last result with timestamp for display
func (p *GeekBench6Plugin) formatLastResult() string {
	if p.lastResult == nil {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("FYI, the last execution result was completed on %s:\n",
		p.lastResult.Timestamp.UTC().Format("2006/01/02 at 15:04:05 UTC")))
	output.WriteString(p.formatResult(p.lastResult))

	return output.String()
}

// formatLatestResult formats the latest result with timestamp for display
func (p *GeekBench6Plugin) formatLatestResult(result *GeekBench6Result) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("The latest execution result was completed on %s:\n",
		result.Timestamp.UTC().Format("2006/01/02 at 15:04:05 UTC")))
	output.WriteString(p.formatResult(result))

	return output.String()
}

// init function to auto-register the GeekBench 6 plugin
func init() {
	plugin.RegisterAgentPlugin("geekbench6", func() plugin.Plugin {
		return geekbench6Instance
	})
}
