package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
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

// ConfirmFunc asks the user (or a policy) to approve a dangerous operation.
// It returns true when the operation is approved.
type ConfirmFunc func(toolName, action, detail string) bool

// ConfirmationMode defines how dangerous operations are handled.
type ConfirmationMode string

const (
	// ConfirmAsk prompts the user before dangerous operations (default).
	ConfirmAsk ConfirmationMode = "ask"
	// ConfirmAlways allows dangerous operations without asking.
	ConfirmAlways ConfirmationMode = "always"
	// ConfirmNever denies all dangerous operations.
	ConfirmNever ConfirmationMode = "never"
)

// Policy decides whether a dangerous operation is allowed.
type Policy struct {
	Mode ConfirmationMode
	Ask  ConfirmFunc // used when Mode == ConfirmAsk
}

// approve returns true when the operation is allowed.
func (p Policy) approve(toolName, action, detail string) bool {
	switch p.Mode {
	case ConfirmAlways:
		return true
	case ConfirmNever:
		return false
	default: // ConfirmAsk
		if p.Ask == nil {
			// No approver available (e.g. non-interactive use) — deny.
			return false
		}
		return p.Ask(toolName, action, detail)
	}
}

// Confirmable is implemented by tools that support operation confirmation.
type Confirmable interface {
	SetPolicy(p Policy)
}

// DefaultMaxResultSize is the default limit for tool result size (chars).
const DefaultMaxResultSize = 20000

// Manager manages registered tools.
type Manager struct {
	tools         map[string]Tool
	maxResultSize int
}

// NewManager creates a new ToolManager with built-in tools.
// root restricts the filesystem tool (empty = current working directory),
// allowedCommands is the process tool whitelist (nil = default safe list),
// maxResultSize limits tool result size (0 = DefaultMaxResultSize).
func NewManager(root string, allowedCommands []string, maxResultSize int) *Manager {
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	if maxResultSize <= 0 {
		maxResultSize = DefaultMaxResultSize
	}

	m := &Manager{
		tools:         make(map[string]Tool),
		maxResultSize: maxResultSize,
	}
	m.Register(&EchoTool{})
	m.Register(&CalcTool{})
	m.Register(&TimeTool{})
	m.Register(&WeatherTool{})
	m.Register(NewFileSystemTool(root))
	m.Register(NewProcessTool(30*time.Second, allowedCommands))
	m.Register(NewWebSearchTool())
	m.Register(NewCodeInterpreterTool())
	m.Register(NewGitTool(""))
	m.Register(NewDatabaseTool(100, 30*time.Second))
	return m
}

// Register adds a tool to the manager.
func (m *Manager) Register(t Tool) {
	m.tools[t.Name()] = t
}

// SetPolicy applies the confirmation policy to all tools that support it.
func (m *Manager) SetPolicy(p Policy) {
	for _, t := range m.tools {
		if c, ok := t.(Confirmable); ok {
			c.SetPolicy(p)
		}
	}
}

// Execute runs a tool by name with JSON arguments.
// Oversized results are truncated to maxResultSize characters.
func (m *Manager) Execute(name, argsJSON string) (string, error) {
	t, ok := m.tools[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	result, err := t.Execute(argsJSON)
	if err != nil {
		return "", err
	}
	return truncateResult(result, m.maxResultSize), nil
}

// truncateResult cuts a tool result at max characters, on a rune boundary.
func truncateResult(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "\n... [output truncated]"
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
	return "Calculates a mathematical expression. Supports +, -, *, /, ^ (power), parentheses and unary minus."
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

// FetchWeather returns current weather data (moved to weather.go).
// The mock fallback implementation lives in weather.go.

// parseArgs parses JSON arguments into the target struct.
func parseArgs(argsJSON string, target interface{}) error {
	trimmed := strings.TrimSpace(argsJSON)
	// If it's already a JSON object, unmarshal directly
	if strings.HasPrefix(trimmed, "{") {
		return json.Unmarshal([]byte(trimmed), target)
	}
	// If it's a plain string, wrap it as JSON
	wrapped := fmt.Sprintf(`{"value": %q}`, trimmed)
	return json.Unmarshal([]byte(wrapped), target)
}

// expression parser implements the following grammar:
//
//	expr   := term (('+' | '-') term)*
//	term   := unary (('*' | '/') unary)*
//	unary  := ('-' | '+') unary | power
//	power  := primary ('^' unary)?        (right-associative)
//	primary:= number | '(' expr ')'

type mathToken struct {
	kind  string // "num", "op", "lparen", "rparen"
	value float64
	op    string
}

// tokenizeExpr splits the expression into tokens.
func tokenizeExpr(expr string) ([]mathToken, error) {
	var tokens []mathToken
	i := 0
	for i < len(expr) {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c >= '0' && c <= '9' || c == '.':
			j := i
			for j < len(expr) && (expr[j] >= '0' && expr[j] <= '9' || expr[j] == '.') {
				j++
			}
			val, err := strconv.ParseFloat(expr[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", expr[i:j])
			}
			tokens = append(tokens, mathToken{kind: "num", value: val})
			i = j
		case strings.ContainsRune("+-*/^", rune(c)):
			tokens = append(tokens, mathToken{kind: "op", op: string(c)})
			i++
		case c == '(':
			tokens = append(tokens, mathToken{kind: "lparen"})
			i++
		case c == ')':
			tokens = append(tokens, mathToken{kind: "rparen"})
			i++
		default:
			return nil, fmt.Errorf("unexpected character: %q", string(c))
		}
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return tokens, nil
}

// mathParser is a recursive descent parser over the token list.
type mathParser struct {
	tokens []mathToken
	pos    int
}

func (p *mathParser) peek() *mathToken {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

func (p *mathParser) next() *mathToken {
	t := p.peek()
	if t != nil {
		p.pos++
	}
	return t
}

// evaluateExpression evaluates a math expression with parentheses, unary
// minus and power operator.
func evaluateExpression(expr string) (float64, error) {
	tokens, err := tokenizeExpr(strings.TrimSpace(expr))
	if err != nil {
		return 0, err
	}
	p := &mathParser{tokens: tokens}
	val, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.peek() != nil {
		return 0, fmt.Errorf("unexpected token at position %d", p.pos)
	}
	return math.Round(val*10000) / 10000, nil
}

// parseExpr := term (('+' | '-') term)*
func (p *mathParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" || (t.op != "+" && t.op != "-") {
			return left, nil
		}
		p.next()
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if t.op == "+" {
			left += right
		} else {
			left -= right
		}
	}
}

// parseTerm := unary (('*' | '/') unary)*
func (p *mathParser) parseTerm() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" || (t.op != "*" && t.op != "/") {
			return left, nil
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		if t.op == "*" {
			left *= right
		} else {
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		}
	}
}

// parseUnary := ('-' | '+') unary | power
func (p *mathParser) parseUnary() (float64, error) {
	t := p.peek()
	if t != nil && t.kind == "op" && (t.op == "-" || t.op == "+") {
		p.next()
		val, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		if t.op == "-" {
			return -val, nil
		}
		return val, nil
	}
	return p.parsePower()
}

// parsePower := primary ('^' unary)?   (right-associative)
func (p *mathParser) parsePower() (float64, error) {
	base, err := p.parsePrimary()
	if err != nil {
		return 0, err
	}
	t := p.peek()
	if t != nil && t.kind == "op" && t.op == "^" {
		p.next()
		exp, err := p.parseUnary() // right-associative: 2^3^2 = 2^(3^2)
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

// parsePrimary := number | '(' expr ')'
func (p *mathParser) parsePrimary() (float64, error) {
	t := p.next()
	if t == nil {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case "num":
		return t.value, nil
	case "lparen":
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if closing := p.next(); closing == nil || closing.kind != "rparen" {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		return val, nil
	default:
		return 0, fmt.Errorf("unexpected token %q", t.op)
	}
}
