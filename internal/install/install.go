// Package install safely updates client configuration and installs embedded skills.
package install

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	assets "github.com/stufently/selectel-s3-mcp-ai-cli/skills"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Options struct {
	Client, Scope, Home, Project, Binary string
	Args                                 []string
	Env                                  map[string]string
	DryRun, Force                        bool
}

func ConfigPath(o Options) (string, error) {
	home := o.Home
	if home == "" {
		var e error
		home, e = os.UserHomeDir()
		if e != nil {
			return "", e
		}
	}
	switch o.Client {
	case "cursor":
		return filepath.Join(home, ".cursor", "mcp.json"), nil
	case "codex":
		return filepath.Join(home, ".codex", "config.toml"), nil
	case "claude-code":
		if o.Scope == "user" {
			return "", nil
		}
		project := o.Project
		if project == "" {
			var e error
			project, e = os.Getwd()
			if e != nil {
				return "", e
			}
		}
		return filepath.Join(project, ".mcp.json"), nil
	case "claude-desktop":
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
		case "windows":
			base := os.Getenv("APPDATA")
			if base == "" {
				base = filepath.Join(home, "AppData", "Roaming")
			}
			return filepath.Join(base, "Claude", "claude_desktop_config.json"), nil
		default:
			return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
		}
	}
	return "", fmt.Errorf("client must be cursor, claude-desktop, codex or claude-code")
}
func atomicWrite(path string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	temp, e := os.CreateTemp(filepath.Dir(path), ".install-")
	if e != nil {
		return e
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if e = temp.Chmod(0600); e != nil {
		_ = temp.Close()
		return e
	}
	_, e = temp.Write(data)
	if e == nil {
		e = temp.Sync()
	}
	ce := temp.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(temp.Name(), path)
}
func MCP(p provider.Profile, o Options) (string, error) {
	if o.Scope != "" && o.Scope != "user" && o.Scope != "project" {
		return "", fmt.Errorf("scope must be user or project")
	}
	if !filepath.IsAbs(o.Binary) {
		return "", fmt.Errorf("binary path must be absolute")
	}
	if len(o.Args) == 0 || o.Args[0] != "mcp" {
		return "", fmt.Errorf("client command must start with mcp")
	}
	allowed := map[string]bool{}
	for _, suffix := range []string{"POOL", "ENDPOINT", "ALLOW_INSECURE", "ALLOW", "DESTRUCTIVE_POLICY", "ALLOWED_BUCKETS", "ALLOWED_PREFIXES", "FILE_ROOT", "AUDIT_FILE"} {
		allowed[p.EnvPrefix+suffix] = true
	}
	for key := range o.Env {
		if !allowed[key] {
			return "", fmt.Errorf("only nonsecret provider environment variables may be installed")
		}
	}
	if o.Client == "claude-code" && o.Scope == "user" {
		quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
		command := "claude mcp add --scope user --transport stdio"
		keys := make([]string, 0, len(o.Env))
		for key := range o.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			command += " --env " + quote(key+"="+o.Env[key])
		}
		command += " " + quote(p.Binary) + " -- " + quote(o.Binary)
		for _, arg := range o.Args {
			command += " " + quote(arg)
		}
		return command + "\n", nil
	}
	path, e := ConfigPath(o)
	if e != nil {
		return "", e
	}
	old, e := os.ReadFile(path) //nolint:gosec // ConfigPath derives the explicitly selected client configuration.
	exists := e == nil
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	entry := map[string]any{"command": o.Binary, "args": o.Args}
	if len(o.Env) > 0 {
		entry["env"] = o.Env
	}
	var data []byte
	if o.Client == "codex" {
		var tree map[string]any
		if len(old) > 0 {
			if e = toml.Unmarshal(old, &tree); e != nil {
				return "", fmt.Errorf("existing TOML is invalid: %w", e)
			}
		}
		present := false
		if servers, ok := tree["mcp_servers"].(map[string]any); ok {
			_, present = servers[p.Binary]
		}
		begin := "# >>> " + p.Binary + " (managed)"
		end := "# <<< " + p.Binary + " (managed)"
		encoded, e := toml.Marshal(map[string]any{"mcp_servers": map[string]any{p.Binary: entry}})
		if e != nil {
			return "", e
		}
		block := begin + "\n" + string(encoded) + end + "\n"
		text := string(old)
		start, finish := strings.Index(text, begin), strings.Index(text, end)
		if present && !o.Force {
			return "", fmt.Errorf("client table exists; use --force to replace only its managed block")
		}
		if present && (start < 0 || finish < start) {
			return "", fmt.Errorf("existing table is not managed; refusing replacement outside markers")
		}
		if start >= 0 || finish >= 0 {
			if start < 0 || finish < start || strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
				return "", fmt.Errorf("invalid managed markers")
			}
			finish += len(end)
			if finish < len(text) && text[finish] == '\n' {
				finish++
			}
			text = text[:start] + block + text[finish:]
		} else {
			if len(text) > 0 && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += block
		}
		data = []byte(text)
		if e = toml.Unmarshal(data, &tree); e != nil {
			return "", fmt.Errorf("refusing invalid resulting TOML: %w", e)
		}
	} else {
		tree := map[string]any{}
		if len(old) > 0 {
			if e = json.Unmarshal(old, &tree); e != nil {
				return "", fmt.Errorf("existing JSON is invalid: %w", e)
			}
			if tree == nil {
				return "", fmt.Errorf("existing JSON must be an object")
			}
		}
		servers, ok := tree["mcpServers"].(map[string]any)
		if !ok {
			if _, present := tree["mcpServers"]; present {
				return "", fmt.Errorf("mcpServers must be an object")
			}
			servers = map[string]any{}
			tree["mcpServers"] = servers
		}
		servers[p.Binary] = entry
		data, e = json.MarshalIndent(tree, "", "  ")
		if e != nil {
			return "", e
		}
		data = append(data, '\n')
	}
	if o.DryRun {
		return string(data), nil
	}
	if string(old) == string(data) {
		return path + " (unchanged)\n", nil
	}
	if exists {
		if e = atomicWrite(path+".bak", old); e != nil {
			return "", e
		}
	}
	if e = atomicWrite(path, data); e != nil {
		return "", e
	}
	return path + "\n", nil
}
func Skill(target string) error {
	return fs.WalkDir(assets.Files, ".", func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		_, relative, ok := strings.Cut(path, "/")
		if !ok {
			return fmt.Errorf("invalid embedded skill path")
		}
		data, e := assets.Files.ReadFile(path)
		if e != nil {
			return e
		}
		out := filepath.Join(target, filepath.FromSlash(relative))
		current, e := os.ReadFile(out) //nolint:gosec // Explicit skill target plus paths from the trusted embedded filesystem.
		if e == nil && string(current) == string(data) {
			return nil
		}
		return atomicWrite(out, data)
	})
}
