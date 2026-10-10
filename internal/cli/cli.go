// Package cli implements the human and script interface.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/analytics"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/install"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/mcpserver"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"golang.org/x/term"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

type app struct {
	p               provider.Profile
	version, commit string
	override        config.Overrides
	json, yes       bool
}

func New(p provider.Profile, version, commit string) *cobra.Command {
	a := &app{p: p, version: version, commit: commit}
	root := &cobra.Command{Use: p.Binary, Short: p.DisplayName + " S3 MCP server and CLI", SilenceErrors: true, SilenceUsage: true}
	flags := root.PersistentFlags()
	flags.BoolVar(&a.json, "json", false, "Print JSON")
	flags.StringVar(&a.override.Pool, "pool", "", "Signing pool")
	flags.StringVar(&a.override.Endpoint, "endpoint", "", "Override endpoint")
	flags.BoolVar(&a.override.AllowInsecure, "allow-insecure", false, "Allow HTTP endpoints")
	flags.StringVar(&a.override.Policy, "destructive-policy", "", "confirm|elicit|block|allow")
	flags.StringVar(&a.override.FileRoot, "file-root", "", "Confined MCP file root")
	flags.BoolVar(&a.yes, "yes", false, "Approve the requested destructive action")
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, e := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s)\n", p.Binary, version, commit)
		return e
	}})
	compat := &cobra.Command{Use: "compat", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, e := io.WriteString(cmd.OutOrStdout(), Compat(p))
		return e
	}}
	compat.Flags().Bool("markdown", false, "Print Markdown compatibility table")
	root.AddCommand(compat)
	a.addAuth(root)
	a.addMCP(root)
	a.addOperations(root)
	a.addTransfers(root)
	skill := &cobra.Command{Use: "skill"}
	target := ""
	skillInstall := &cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if target == "" {
			home, e := os.UserHomeDir()
			if e != nil {
				return e
			}
			target = filepath.Join(home, ".claude", "skills", p.Binary)
		}
		if e := install.Skill(target); e != nil {
			return e
		}
		_, e := fmt.Fprintln(cmd.OutOrStdout(), target)
		return e
	}}
	skillInstall.Flags().StringVar(&target, "target", "", "Target directory")
	skill.AddCommand(skillInstall)
	root.AddCommand(skill)
	root.AddCommand(&cobra.Command{Use: "doctor", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return a.doctor(cmd) }})
	return root
}
func (a *app) client() (*s3core.Client, error) {
	c, e := config.Load(a.p, a.override)
	if e != nil {
		return nil, e
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return nil, fmt.Errorf("credentials are not configured; run %s auth set", a.p.Binary)
	}
	c.Allow = map[string]bool{"write": true, "delete": true, "admin": true}
	return s3core.New(c, a.p)
}
func (a *app) print(cmd *cobra.Command, out any) error {
	data, e := json.Marshal(out)
	if e != nil {
		return e
	}
	if !a.json {
		data, e = json.MarshalIndent(out, "", "  ")
		if e != nil {
			return e
		}
	}
	_, e = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return e
}
func (a *app) invoke(cmd *cobra.Command, name string, args s3core.Args) error {
	c, e := a.client()
	if e != nil {
		return e
	}
	if s3core.Gated(name, args) {
		if e = a.confirm(cmd, fmt.Sprintf("%s on %s/%s", name, args.Bucket, args.Key)); e != nil {
			return e
		}
		args.Confirm = true
	}
	var out any
	if name == "s3_prefix_usage" {
		out, e = analytics.Query(cmd.Context(), c, name, args)
	} else {
		out, e = c.Execute(cmd.Context(), name, args, nil)
	}
	if e != nil {
		return s3core.MapError(e, c.Config.SecretKey, c.Config.AccessKey)
	}
	return a.print(cmd, out)
}
func (a *app) confirm(cmd *cobra.Command, effect string) error {
	if a.yes {
		return nil
	}
	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return &ExitError{Code: 2, Message: "human confirmation required on non-TTY; use --yes: " + effect}
	}
	if _, e := fmt.Fprintf(cmd.ErrOrStderr(), "%s? [y/N] ", effect); e != nil {
		return e
	}
	line, e := bufio.NewReader(input).ReadString('\n')
	if e != nil {
		return e
	}
	if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
		return &ExitError{Code: 2, Message: "operation declined"}
	}
	return nil
}
func (a *app) addAuth(root *cobra.Command) {
	auth := &cobra.Command{Use: "auth"}
	auth.AddCommand(&cobra.Command{Use: "set", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		reader := bufio.NewReader(cmd.InOrStdin())
		read := func(label string) (string, error) {
			if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
				if _, e := fmt.Fprint(cmd.ErrOrStderr(), label+": "); e != nil {
					return "", e
				}
				data, e := term.ReadPassword(int(file.Fd()))
				_, _ = fmt.Fprintln(cmd.ErrOrStderr())
				return string(data), e
			}
			data, e := reader.ReadString('\n')
			if e == io.EOF && len(data) > 0 {
				e = nil
			}
			return strings.TrimRight(data, "\r\n"), e
		}
		key, e := read("Access key ID")
		if e != nil {
			return e
		}
		secret, e := read("Secret access key")
		if e != nil {
			return e
		}
		if e = config.SaveCredentials(a.p, key, secret); e != nil {
			return e
		}
		_, e = fmt.Fprintln(cmd.OutOrStdout(), "Credentials saved (0600)")
		return e
	}})
	auth.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, e := config.Load(a.p, a.override)
		if e != nil {
			return e
		}
		path, e := config.CredentialsPath(a.p)
		if e != nil {
			return e
		}
		mode := "absent"
		if info, e := os.Stat(path); e == nil {
			mode = info.Mode().Perm().String()
		}
		return a.print(cmd, map[string]any{"source": c.Source, "fingerprint": c.Fingerprint(), "credential_file": path, "permissions": mode})
	}})
	root.AddCommand(auth)
}
func (a *app) addMCP(root *cobra.Command) {
	mcpCmd := &cobra.Command{Use: "mcp", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, e := config.Load(a.p, a.override)
		if e != nil {
			return e
		}
		if c.AccessKey == "" || c.SecretKey == "" {
			return fmt.Errorf("credentials not configured")
		}
		core, e := s3core.New(c, a.p)
		if e != nil {
			return e
		}
		server, e := mcpserver.New(a.p, c, core, a.version)
		if e != nil {
			return e
		}
		defer func() { _ = server.Close() }()
		return runStdio(cmd.Context(), server)
	}}
	mcpCmd.PersistentFlags().BoolVar(&a.override.AllowWrite, "allow-write", false, "Register write tools")
	mcpCmd.PersistentFlags().BoolVar(&a.override.AllowDelete, "allow-delete", false, "Register delete tools")
	mcpCmd.PersistentFlags().BoolVar(&a.override.AllowAdmin, "allow-admin", false, "Register admin tools")
	o := install.Options{}
	installCmd := &cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		binary, e := os.Executable()
		if e != nil {
			return e
		}
		binary, e = filepath.Abs(binary)
		if e != nil {
			return e
		}
		o.Binary = binary
		o.Args = []string{"mcp"}
		c, e := config.Load(a.p, a.override)
		if e != nil {
			return e
		}
		for _, level := range []string{"write", "delete", "admin"} {
			if c.Allow[level] {
				o.Args = append(o.Args, "--allow-"+level)
			}
		}
		o.Env = map[string]string{a.p.EnvPrefix + "POOL": c.Pool, a.p.EnvPrefix + "ENDPOINT": c.Endpoint, a.p.EnvPrefix + "DESTRUCTIVE_POLICY": c.Policy}
		if c.AllowInsecure {
			o.Env[a.p.EnvPrefix+"ALLOW_INSECURE"] = "1"
		}
		if c.FileRoot != "" {
			o.Env[a.p.EnvPrefix+"FILE_ROOT"] = c.FileRoot
		}
		if c.AuditFile != "" {
			o.Env[a.p.EnvPrefix+"AUDIT_FILE"] = c.AuditFile
		}
		if len(c.Buckets) > 0 {
			o.Env[a.p.EnvPrefix+"ALLOWED_BUCKETS"] = strings.Join(c.Buckets, ",")
		}
		if len(c.Prefixes) > 0 {
			o.Env[a.p.EnvPrefix+"ALLOWED_PREFIXES"] = strings.Join(c.Prefixes, ",")
		}
		out, e := install.MCP(a.p, o)
		if e != nil {
			return e
		}
		_, e = io.WriteString(cmd.OutOrStdout(), out)
		return e
	}}
	f := installCmd.Flags()
	f.StringVar(&o.Client, "client", "", "cursor|claude-desktop|codex|claude-code")
	f.StringVar(&o.Scope, "scope", "user", "user|project")
	f.BoolVar(&o.DryRun, "dry-run", false, "Print without writing")
	f.BoolVar(&o.Force, "force", false, "Replace owned managed block")
	mcpCmd.AddCommand(installCmd)
	root.AddCommand(mcpCmd)
}
func (a *app) addOperations(root *cobra.Command) {
	lsArgs := s3core.Args{}
	ls := &cobra.Command{Use: "ls [bucket[/prefix]]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := "s3_list_buckets"
		if len(args) > 0 {
			var e error
			lsArgs.Bucket, lsArgs.Prefix, e = remote(args[0], a.p.URIScheme, true)
			if e != nil {
				return e
			}
			name = "s3_list_objects"
		}
		return a.invoke(cmd, name, lsArgs)
	}}
	ls.Flags().Int32Var(&lsArgs.MaxKeys, "max-keys", 1000, "Page size 1..1000")
	ls.Flags().StringVar(&lsArgs.Token, "continuation-token", "", "Next-page token")
	root.AddCommand(ls)
	for _, pair := range []struct{ command, tool string }{{"stat", "s3_head_object"}, {"versions", "s3_list_object_versions"}, {"du", "s3_prefix_usage"}, {"mb", "s3_create_bucket"}, {"rb", "s3_delete_bucket"}, {"rm", "s3_delete_object"}} {
		args := s3core.Args{}
		cmd := &cobra.Command{Use: pair.command + " bucket[/key]", Args: cobra.ExactArgs(1)}
		cmd.RunE = func(cmd *cobra.Command, values []string) error {
			var e error
			args.Bucket, args.Key, e = remote(values[0], a.p.URIScheme, true)
			if e != nil {
				return e
			}
			if pair.command == "du" || pair.command == "versions" {
				args.Prefix = args.Key
				args.Key = ""
			}
			if (pair.command == "mb" || pair.command == "rb") && args.Key != "" {
				return fmt.Errorf("bucket command does not accept key")
			}
			return a.invoke(cmd, pair.tool, args)
		}
		if pair.command == "mb" {
			cmd.Flags().BoolVar(&args.ObjectLock, "object-lock", false, "Enable Object Lock at creation")
		}
		if pair.command == "stat" || pair.command == "rm" {
			cmd.Flags().StringVar(&args.VersionID, "version-id", "", "Explicit version")
		}
		if pair.command == "versions" {
			cmd.Flags().Int32Var(&args.MaxKeys, "max-keys", 1000, "Page size")
			cmd.Flags().StringVar(&args.Token, "continuation-token", "", "Key marker")
			cmd.Flags().StringVar(&args.VersionMarker, "version-marker", "", "Version marker")
		}
		if pair.command == "du" {
			cmd.Flags().IntVar(&args.MaxScan, "max-scan", 10000, "Bounded scan")
			cmd.Flags().IntVar(&args.Offset, "result-offset", 0, "Aggregate row offset")
		}
		root.AddCommand(cmd)
	}
	catArgs := s3core.Args{}
	cat := &cobra.Command{Use: "cat bucket/key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var e error
		catArgs.Bucket, catArgs.Key, e = remote(args[0], a.p.URIScheme, true)
		if e != nil {
			return e
		}
		c, e := a.client()
		if e != nil {
			return e
		}
		if a.json {
			return a.invoke(cmd, "s3_get_object", catArgs)
		}
		e = c.DownloadWriter(cmd.Context(), catArgs, cmd.OutOrStdout())
		if e != nil {
			return s3core.MapError(e, c.Config.SecretKey, c.Config.AccessKey)
		}
		return nil
	}}
	cat.Flags().StringVar(&catArgs.VersionID, "version-id", "", "Explicit version")
	root.AddCommand(cat)
	presignArgs := s3core.Args{}
	method := "GET"
	presign := &cobra.Command{Use: "presign bucket/key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var e error
		presignArgs.Bucket, presignArgs.Key, e = remote(args[0], a.p.URIScheme, true)
		if e != nil {
			return e
		}
		tool := "s3_presign_get"
		if strings.EqualFold(method, "PUT") {
			tool = "s3_presign_put"
		} else if !strings.EqualFold(method, "GET") {
			return fmt.Errorf("method must be GET or PUT")
		}
		c, e := a.client()
		if e != nil {
			return e
		}
		if s3core.Gated(tool, presignArgs) {
			if e = a.confirm(cmd, "signed PUT may overwrite "+presignArgs.Bucket+"/"+presignArgs.Key); e != nil {
				return e
			}
			presignArgs.Confirm = true
		}
		out, e := c.Execute(cmd.Context(), tool, presignArgs, nil)
		if e != nil {
			return s3core.MapError(e, c.Config.SecretKey, c.Config.AccessKey)
		}
		if a.json {
			return a.print(cmd, out)
		}
		_, e = fmt.Fprintln(cmd.OutOrStdout(), out.(map[string]any)["url"])
		return e
	}}
	presign.Flags().StringVar(&method, "method", "GET", "GET|PUT")
	presign.Flags().Int64Var(&presignArgs.Expires, "expires", 3600, "Lifetime seconds")
	root.AddCommand(presign)
}
func remote(value, scheme string, plain bool) (string, string, error) {
	if strings.HasPrefix(value, "s3://") {
		value = strings.TrimPrefix(value, "s3://")
	} else if strings.HasPrefix(value, scheme+"://") {
		value = strings.TrimPrefix(value, scheme+"://")
	} else if !plain {
		return "", "", fmt.Errorf("use s3://bucket/key for S3 paths")
	}
	bucket, key, _ := strings.Cut(value, "/")
	if bucket == "" || strings.ContainsAny(bucket, ":\\") {
		return "", "", fmt.Errorf("invalid bucket path")
	}
	return bucket, key, nil
}
func (a *app) doctor(cmd *cobra.Command) error {
	c, e := a.client()
	if e != nil {
		return e
	}
	u, e := url.Parse(c.Config.Endpoint)
	if e != nil {
		return e
	}
	addresses, e := net.DefaultResolver.LookupHost(cmd.Context(), u.Hostname())
	if e != nil {
		return e
	}
	date, e := s3core.EndpointDate(cmd.Context(), c.Config.Endpoint)
	if e != nil {
		return e
	}
	skew := time.Since(date)
	if skew < 0 {
		skew = -skew
	}
	status := "ok"
	if skew > 5*time.Minute {
		status = "warning"
	}
	out := map[string]any{"credential_source": c.Config.Source, "fingerprint": c.Config.Fingerprint(), "endpoint": c.Config.Endpoint, "dns": addresses, "tls": u.Scheme == "https", "clock_skew_seconds": skew.Seconds(), "clock_status": status}
	path, e := config.CredentialsPath(a.p)
	if e != nil {
		return e
	}
	if info, e := os.Stat(path); e == nil {
		out["credential_permissions"] = info.Mode().Perm().String()
	} else {
		out["credential_permissions"] = "absent"
	}
	if skew > 15*time.Minute {
		out["clock_status"] = "error"
		if e = a.print(cmd, out); e != nil {
			return e
		}
		return fmt.Errorf("clock skew exceeds 15 minutes")
	}
	buckets, e := c.Execute(cmd.Context(), "s3_list_buckets", s3core.Args{}, nil)
	if e != nil {
		return s3core.MapError(e, c.Config.SecretKey, c.Config.AccessKey)
	}
	out["buckets"] = buckets
	return a.print(cmd, out)
}

var _ = context.Background
