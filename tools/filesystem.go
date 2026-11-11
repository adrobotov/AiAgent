package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxReadFileSize is the maximum file size allowed for read operations (64 MB).
const MaxReadFileSize = 64 * 1024 * 1024

// FileSystemTool provides file system operations.
type FileSystemTool struct {
	// Root restricts operations to a specific directory.
	// If empty, all paths are allowed.
	Root string

	// policy controls confirmation of dangerous operations.
	policy Policy

	// autoConfirm bypasses confirmation when true.
	autoConfirm bool
}

// NewFileSystemTool creates a new file system tool with optional root restriction.
func NewFileSystemTool(root string, autoConfirm bool) *FileSystemTool {
	return &FileSystemTool{
		Root:        root,
		autoConfirm: autoConfirm,
	}
}

// SetPolicy sets the confirmation policy for dangerous operations.
func (t *FileSystemTool) SetPolicy(p Policy) {
	t.policy = p
}

// Name returns the tool name.
func (t *FileSystemTool) Name() string {
	return "filesystem"
}

// Description returns the tool description.
func (FileSystemTool) Description() string {
	return "Performs file system operations: read, write, create, append, insert, replace, search, list, delete, mkdir, exists, copy, move, and get info."
}

// Parameters returns the tool parameters schema.
func (FileSystemTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"read", "write", "create", "append", "insert", "replace", "search", "list", "delete", "mkdir", "exists", "copy", "move", "info"},
				"description": "The file system operation to perform.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file or directory path.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write/append/insert (for 'write', 'create', 'append', 'insert' actions).",
			},
			"search": map[string]interface{}{
				"type":        "string",
				"description": "Search pattern (for 'replace' and 'search' actions).",
			},
			"replace": map[string]interface{}{
				"type":        "string",
				"description": "Replacement string (for 'replace' action).",
			},
			"line": map[string]interface{}{
				"type":        "integer",
				"description": "Line number for insert action (1-based).",
			},
			"destination": map[string]interface{}{
				"type":        "string",
				"description": "Destination path (for 'copy' and 'move' actions).",
			},
		},
		"required": []string{"action", "path"},
	}
}

// Execute executes the file system operation.
func (t *FileSystemTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Path        string `json:"path"`
		Content     string `json:"content"`
		Search      string `json:"search"`
		Replace     string `json:"replace"`
		Line        int    `json:"line"`
		Destination string `json:"destination"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	path, err := t.resolvePath(args.Path)
	if err != nil {
		return "", err
	}

	action := strings.ToLower(args.Action)

	// Dangerous operations require confirmation according to the policy.
	if !t.autoConfirm && isDangerousAction(action) {
		detail := path
		switch action {
		case "write", "create", "append", "insert":
			detail = fmt.Sprintf("%s (%d bytes)", path, len(args.Content))
		case "replace":
			detail = fmt.Sprintf("%s (replace '%s' with '%s')", path, args.Search, args.Replace)
		}
		if !t.policy.approve(t.Name(), action, detail) {
			return "", fmt.Errorf("operation '%s' on %s was not approved (cancelled)", action, path)
		}
	}

	switch action {
	case "read":
		return t.readFile(path)
	case "write":
		return t.writeFile(path, args.Content)
	case "create":
		return t.createFile(path, args.Content)
	case "append":
		return t.appendFile(path, args.Content)
	case "insert":
		return t.insertLine(path, args.Line, args.Content)
	case "replace":
		return t.replaceText(path, args.Search, args.Replace)
	case "search":
		return t.searchFile(path, args.Search)
	case "list":
		return t.listDir(path)
	case "delete":
		return t.deletePath(path)
	case "mkdir":
		return t.mkdir(path)
	case "exists":
		return t.exists(path)
	case "copy":
		dest, err := t.resolvePath(args.Destination)
		if err != nil {
			return "", err
		}
		return t.copyFile(path, dest)
	case "move":
		dest, err := t.resolvePath(args.Destination)
		if err != nil {
			return "", err
		}
		return t.moveFile(path, dest)
	case "info":
		return t.getFileinfo(path)
	default:
		return "", fmt.Errorf("unknown action: %s. Available: read, write, create, append, insert, replace, search, list, delete, mkdir, exists, copy, move, info", args.Action)
	}
}

// resolvePath resolves and validates a path, checking root restriction.
// An empty path refers to the root directory when a root is configured.
func (t *FileSystemTool) resolvePath(path string) (string, error) {
	if path == "" {
		if t.Root == "" {
			return "", fmt.Errorf("path is required")
		}
		root, err := filepath.Abs(t.Root)
		if err != nil {
			return "", fmt.Errorf("invalid root path: %w", err)
		}
		return root, nil
	}

	if t.Root != "" {
		// Resolve root to absolute path
		root, err := filepath.Abs(t.Root)
		if err != nil {
			return "", fmt.Errorf("invalid root path: %w", err)
		}

		var resolved string
		if filepath.IsAbs(path) {
			// Absolute path: check if it's within root
			resolved = filepath.Clean(path)
		} else {
			// Relative path: join with root
			resolved = filepath.Clean(filepath.Join(root, path))
		}

		// Ensure the resolved path is within the root.
		// Use filepath.Rel which returns ".." prefix for paths outside root.
		rel, err := filepath.Rel(root, resolved)
		if err != nil {
			return "", fmt.Errorf("path outside root directory")
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return "", fmt.Errorf("path outside root directory")
		}

		return resolved, nil
	}

	// No root restriction — still clean and resolve the path
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("invalid path: %w", err)
		}
		return abs, nil
	}
	return path, nil
}

// readFile reads a file and returns its contents.
// Large files are rejected to prevent resource exhaustion.
func (t *FileSystemTool) readFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.Size() > MaxReadFileSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), MaxReadFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}

// writeFile writes content to a file, creating it if it doesn't exist.
func (t *FileSystemTool) writeFile(path string, content string) (string, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
}

// listDir lists directory contents.
func (t *FileSystemTool) listDir(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
	}

	var parts []string
	for _, entry := range entries {
		info, _ := entry.Info()
		mode := info.Mode()
		prefix := " "
		if entry.IsDir() {
			prefix = "D"
		} else if mode&0o111 != 0 {
			prefix = "X"
		}
		parts = append(parts, fmt.Sprintf("[%s] %s (%d bytes)", prefix, entry.Name(), info.Size()))
	}

	if len(parts) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(parts, "\n"), nil
}

// deletePath deletes a file or directory.
func (t *FileSystemTool) deletePath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to access path: %w", err)
	}

	if info.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			return "", fmt.Errorf("failed to delete directory: %w", err)
		}
		return fmt.Sprintf("Directory deleted: %s", path), nil
	}

	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("failed to delete file: %w", err)
	}
	return fmt.Sprintf("File deleted: %s", path), nil
}

// mkdir creates a directory.
func (t *FileSystemTool) mkdir(path string) (string, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	return fmt.Sprintf("Directory created: %s", path), nil
}

// exists checks if a path exists.
func (t *FileSystemTool) exists(path string) (string, error) {
	_, err := os.Stat(path)
	if err == nil {
		return "true", nil
	}
	if os.IsNotExist(err) {
		return "false", nil
	}
	return fmt.Sprintf("error: %v", err), nil
}

// copyFile copies a file to a new location.
func (t *FileSystemTool) copyFile(src, dst string) (string, error) {
	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("failed to read source: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Write destination
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write destination: %w", err)
	}
	return fmt.Sprintf("Copied %s -> %s (%d bytes)", src, dst, len(data)), nil
}

// moveFile moves a file to a new location.
func (t *FileSystemTool) moveFile(src, dst string) (string, error) {
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("failed to move file: %w", err)
	}
	return fmt.Sprintf("Moved %s -> %s", src, dst), nil
}

// getFileinfo returns file metadata.
func (t *FileSystemTool) getFileinfo(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %w", err)
	}

	mode := info.Mode()
	typeStr := "file"
	if info.IsDir() {
		typeStr = "directory"
	}

	return fmt.Sprintf(
		"name: %s\ntype: %s\nsize: %d bytes\nmodified: %s\npermissions: %s",
		info.Name(),
		typeStr,
		info.Size(),
		info.ModTime().Format("2006-01-02 15:04:05"),
		mode.String(),
	), nil
}

// createFile creates a new file with the given content.
// Returns an error if the file already exists.
func (t *FileSystemTool) createFile(path string, content string) (string, error) {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("file already exists: %s", path)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	return fmt.Sprintf("Created new file: %s (%d bytes)", path, len(content)), nil
}

// appendFile appends content to the end of an existing file.
func (t *FileSystemTool) appendFile(path string, content string) (string, error) {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s (use 'create' to create new files)", path)
		}
		return "", fmt.Errorf("failed to access file: %w", err)
	}

	// Append to existing file
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open file for appending: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(content); err != nil {
		return "", fmt.Errorf("failed to append content: %w", err)
	}

	return fmt.Sprintf("Appended %d bytes to %s (total size: %d bytes)", len(content), path, info.Size()+int64(len(content))), nil
}

// insertLine inserts content at the specified line number (1-based).
// If line is 0, inserts at the beginning. If line > total lines, appends at the end.
func (t *FileSystemTool) insertLine(path string, lineNum int, content string) (string, error) {
	// Read existing content
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s (use 'create' to create new files)", path)
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty element from split if file ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	totalLines := len(lines)

	// Validate line number
	if lineNum < 0 {
		return "", fmt.Errorf("line number must be >= 0")
	}

	var newLines []string
	if lineNum == 0 || lineNum > totalLines {
		// Insert at beginning or append at end
		if lineNum == 0 {
			newLines = append([]string{content}, lines...)
		} else {
			newLines = append(lines, content)
		}
	} else {
		// Insert at specific line
		newLines = append(lines[:lineNum-1], append([]string{content}, lines[lineNum-1:]...)...)
	}

	// Write back
	result := strings.Join(newLines, "\n")
	if err := os.WriteFile(path, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Inserted at line %d: %s (%d lines total)", lineNum, path, len(newLines)), nil
}

// replaceText replaces all occurrences of search pattern with replace string.
// Supports both plain text and regex patterns.
func (t *FileSystemTool) replaceText(path, search, replace string) (string, error) {
	// Read existing content
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", path)
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)

	// Try regex first, fall back to plain text
	re, err := regexp.Compile(search)
	if err != nil {
		// Use plain text replacement
		count := strings.Count(content, search)
		if count == 0 {
			return fmt.Sprintf("No occurrences of '%s' found in %s", search, path), nil
		}
		newContent := strings.ReplaceAll(content, search, replace)
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Replaced %d occurrences of '%s' with '%s' in %s", count, search, replace, path), nil
	}

	// Use regex replacement
	count := len(re.FindAllString(content, -1))
	if count == 0 {
		return fmt.Sprintf("No matches for '%s' found in %s", search, path), nil
	}
	newContent := re.ReplaceAllString(content, replace)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Replaced %d regex matches of '%s' with '%s' in %s", count, search, replace, path), nil
}

// searchFile searches for a pattern in the file and returns matching lines.
func (t *FileSystemTool) searchFile(path, pattern string) (string, error) {
	// Read existing content
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", path)
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var matches []string
	matchCount := 0

	for i, line := range lines {
		// Try regex first
		re, err := regexp.Compile(pattern)
		if err == nil {
			if re.MatchString(line) {
				matchCount++
				matches = append(matches, fmt.Sprintf("%d: %s", i+1, line))
				continue
			}
		}
		// Fall back to plain text
		if strings.Contains(line, pattern) {
			matchCount++
			matches = append(matches, fmt.Sprintf("%d: %s", i+1, line))
		}
	}

	if matchCount == 0 {
		return fmt.Sprintf("No matches for '%s' in %s", pattern, path), nil
	}

	result := fmt.Sprintf("Found %d matches in %s:\n", matchCount, path)
	if len(matches) <= 50 {
		result += strings.Join(matches, "\n")
	} else {
		result += strings.Join(matches[:50], "\n")
		result += fmt.Sprintf("\n... and %d more matches", matchCount-50)
	}
	return result, nil
}

// isDangerousAction returns true for operations that modify files or directories.
func isDangerousAction(action string) bool {
	switch action {
	case "write", "create", "delete", "move", "append", "insert", "replace":
		return true
	}
	return false
}
