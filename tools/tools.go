package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"agent/llm"
)

// Tool represents a callable function.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(argsJSON string) (string, error)
}

// Manager manages registered tools.
type Manager struct {
	tools map[string]Tool
}

// NewManager creates a new ToolManager with built-in tools.
func NewManager() *Manager {
	m := &Manager{
		tools: make(map[string]Tool),
	}
	m.Register(&EchoTool{})
	m.Register(&CalcTool{})
	m.Register(&TimeTool{})
	m.Register(&WeatherTool{})
	m.Register(NewWebSearchTool())
	m.Register(NewCodeInterpreterTool())
	return m
}

// Register adds a tool to the manager.
func (m *Manager) Register(t Tool) {
	m.tools[t.Name()] = t
}

// Execute runs a tool by name with JSON arguments.
func (m *Manager) Execute(name, argsJSON string) (string, error) {
	t, ok := m.tools[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return t.Execute(argsJSON)
}

// ToFunctionDefinitions converts registered tools to GigaChat function definitions.
func (m *Manager) ToFunctionDefinitions() []llm.FunctionDefinition {
	functions := make([]llm.FunctionDefinition, 0, len(m.tools))
	for _, t := range m.tools {
		functions = append(functions, llm.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return functions
}

// --- Built-in Tools ---

// EchoTool returns the input message.
type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo" }
func (t *EchoTool) Description() string {
	return "Echoes back the provided message."
}
func (t *EchoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The message to echo back.",
			},
		},
		"required": []string{"message"},
	}
}
func (t *EchoTool) Execute(argsJSON string) (string, error) {
	return ExecuteEcho(argsJSON)
}

// CalcTool performs mathematical calculations.
type CalcTool struct{}

func (t *CalcTool) Name() string { return "calc" }
func (t *CalcTool) Description() string {
	return "Calculates a mathematical expression."
}
func (t *CalcTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"expression": map[string]interface{}{
				"type":        "string",
				"description": "The mathematical expression to evaluate (e.g., '2 + 3 * 4').",
			},
		},
		"required": []string{"expression"},
	}
}
func (t *CalcTool) Execute(argsJSON string) (string, error) {
	return Calculate(argsJSON)
}

// TimeTool returns the current time.
type TimeTool struct{}

func (t *TimeTool) Name() string { return "time" }
func (t *TimeTool) Description() string {
	return "Returns the current date and time."
}
func (t *TimeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *TimeTool) Execute(argsJSON string) (string, error) {
	return GetCurrentTime()
}

// WeatherTool fetches weather information (mock).
type WeatherTool struct{}

func (t *WeatherTool) Name() string { return "weather" }
func (t *WeatherTool) Description() string {
	return "Fetches weather information for a location (mock data)."
}
func (t *WeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "The city name to get weather for.",
			},
		},
		"required": []string{"location"},
	}
}
func (t *WeatherTool) Execute(argsJSON string) (string, error) {
	return FetchWeather(argsJSON)
}

// --- Tool Implementations ---

// ExecuteEcho echoes the message from JSON args.
func ExecuteEcho(argsJSON string) (string, error) {
	var args struct {
		Message string `json:"message"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}
	return args.Message, nil
}

// Calculate evaluates a simple math expression.
func Calculate(argsJSON string) (string, error) {
	var args struct {
		Expression string `json:"expression"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	result, err := evaluateExpression(args.Expression)
	if err != nil {
		return "", fmt.Errorf("calculation error: %w", err)
	}
	return fmt.Sprintf("%.4f", result), nil
}

// GetCurrentTime returns the current time.
func GetCurrentTime() (string, error) {
	return time.Now().Format("2006-01-02 15:04:05 MST"), nil
}

// FetchWeather returns mock weather data.
func FetchWeather(argsJSON string) (string, error) {
	var args struct {
		Location string `json:"location"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	// Mock weather data
	weather := map[string]struct {
		Temp    string
		Condition string
	}{
		"москва":   {Temp: "+5°C", Condition: "Облачно"},
		"moscow":   {Temp: "+5°C", Condition: "Облачно"},
		"new york": {Temp: "+15°C", Condition: "Солнечно"},
		"london":   {Temp: "+10°C", Condition: "Дождь"},
		"tokyo":    {Temp: "+20°C", Condition: "Ясно"},
	}

	loc := strings.ToLower(args.Location)
	if w, ok := weather[loc]; ok {
		return fmt.Sprintf("Погода в %s: %s, %s", args.Location, w.Temp, w.Condition), nil
	}

	// Generate mock data for unknown locations
	return fmt.Sprintf("Погода в %s: +12°C, Переменная облачность (моковые данные)", args.Location), nil
}

// parseArgs parses JSON arguments into the target struct.
func parseArgs(argsJSON string, target interface{}) error {
	// If it's already a JSON object, unmarshal directly
	if strings.HasPrefix(argsJSON, "{") {
		return json.Unmarshal([]byte(argsJSON), target)
	}
	// If it's a plain string, wrap it as JSON
	argsJSON = fmt.Sprintf(`{"value": %q}`, argsJSON)
	return json.Unmarshal([]byte(argsJSON), target)
}

// evaluateExpression evaluates a simple math expression.
func evaluateExpression(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)

	type token struct {
		value float64
		op    rune
	}

	var tokens []token
	var numBuf strings.Builder

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		if c >= '0' && c <= '9' || c == '.' {
			numBuf.WriteByte(c)
		} else if c == '+' || c == '-' || c == '*' || c == '/' {
			if numBuf.Len() > 0 {
				val, err := strconv.ParseFloat(numBuf.String(), 64)
				if err != nil {
					return 0, fmt.Errorf("invalid number: %s", numBuf.String())
				}
				tokens = append(tokens, token{value: val})
				numBuf.Reset()
			}
			tokens = append(tokens, token{op: rune(c)})
		}
	}

	if numBuf.Len() > 0 {
		val, err := strconv.ParseFloat(numBuf.String(), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number: %s", numBuf.String())
		}
		tokens = append(tokens, token{value: val})
	}

	if len(tokens) == 0 {
		return 0, fmt.Errorf("empty expression")
	}

	// First pass: handle * and /
	var result []token
	i := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.op == '*' || t.op == '/' {
			if len(result) == 0 || result[len(result)-1].op != 0 {
				return 0, fmt.Errorf("invalid expression")
			}
			prev := result[len(result)-1].value
			result = result[:len(result)-1]

			i++
			if i >= len(tokens) || tokens[i].op != 0 {
				return 0, fmt.Errorf("invalid expression")
			}
			nextVal := tokens[i].value

			var val float64
			if t.op == '*' {
				val = prev * nextVal
			} else {
				if nextVal == 0 {
					return 0, fmt.Errorf("division by zero")
				}
				val = prev / nextVal
			}
			result = append(result, token{value: val})
		} else {
			result = append(result, t)
		}
		i++
	}

	// Second pass: handle + and -
	if len(result) == 0 {
		return 0, fmt.Errorf("invalid expression")
	}

	final := result[0].value
	for i := 1; i+1 < len(result); i += 2 {
		op := result[i].op
		next := result[i+1].value

		switch op {
		case '+':
			final += next
		case '-':
			final -= next
		default:
			return 0, fmt.Errorf("unknown operator: %c", op)
		}
	}

	return math.Round(final*10000) / 10000, nil
}
