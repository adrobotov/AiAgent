package tools

import (
	"fmt"
	"os"
	"path/filepath"
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
}

// NewFileSystemTool creates a new file system tool with optional root restriction.
func NewFileSystemTool(root string) *FileSystemTool {
	return &FileSystemTool{
		Root: root,
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
	return "Performs file system operations: read, write, list, delete, mkdir, exists, copy, move, and get info."
}

// Parameters returns the tool parameters schema.
func (FileSystemTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"read", "write", "list", "delete", "mkdir", "exists", "copy", "move", "info"},
				"description": "The file system operation to perform.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file or directory path.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write (for 'write' action).",
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
	if action == "write" || action == "delete" || action == "move" {
		detail := path
		if action == "write" {
			detail = fmt.Sprintf("%s (%d bytes)", path, len(args.Content))
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
		return "", fmt.Errorf("unknown action: %s. Available: read, write, list, delete, mkdir, exists, copy, move, info", args.Action)
	}
}

// resolvePath resolves and validates a path, checking root restriction
// and resolving symlinks to prevent path traversal attacks.
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

		// Resolve the requested path relative to root
		var resolved string
		if filepath.IsAbs(path) {
			resolved = path
		} else {
			resolved = filepath.Join(root, path)
		}
		resolved = filepath.Clean(resolved)

		// Resolve symlinks in the root to prevent symlink-based escapes
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			// If we can't resolve symlinks, fall back to standard validation
			realRoot = root
		}

		// Ensure the resolved path is within the root
		rel, err := filepath.Rel(realRoot, resolved)
		if err != nil {
			return "", fmt.Errorf("path outside root directory")
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return "", fmt.Errorf("path outside root directory")
		}

		// Also check the original path (before symlink resolution)
		safePath := filepath.Clean(path)
		if !filepath.IsAbs(safePath) {
			safePath = filepath.Join(root, safePath)
		}
		safePath = filepath.Clean(safePath)
		relSafe, _ := filepath.Rel(root, safePath)
		if strings.HasPrefix(relSafe, ".."+string(filepath.Separator)) || relSafe == ".." {
			return "", fmt.Errorf("path outside root directory")
		}

		return resolved, nil
	}

	// No root restriction — still resolve symlinks and clean path
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("invalid path: %w", err)
		}
		return abs, nil
	}
	return filepath.Clean(path), nil
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
