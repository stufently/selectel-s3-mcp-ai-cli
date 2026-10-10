package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (a *app) isRemote(path string) bool {
	return strings.HasPrefix(path, "s3://") || strings.HasPrefix(path, a.p.URIScheme+"://")
}
func (a *app) addTransfers(root *cobra.Command) {
	args := s3core.Args{}
	recursive := false
	cp := &cobra.Command{Use: "cp source destination", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, values []string) error {
		c, e := a.client()
		if e != nil {
			return e
		}
		if args.Overwrite {
			if e = a.confirm(cmd, "overwrite destination "+values[1]); e != nil {
				return e
			}
			args.Confirm = true
		}
		if recursive {
			e = a.copyRecursive(cmd, c, values[0], values[1], args)
		} else {
			e = a.copyOne(cmd, c, values[0], values[1], args)
		}
		if e != nil {
			return s3core.MapError(e, c.Config.SecretKey, c.Config.AccessKey)
		}
		if a.json {
			return a.print(cmd, map[string]any{"source": values[0], "destination": values[1], "copied": true})
		}
		return nil
	}}
	cp.Flags().BoolVar(&recursive, "recursive", false, "Copy directory/prefix recursively")
	cp.Flags().BoolVar(&args.Overwrite, "overwrite", false, "Overwrite existing target with approval")
	cp.Flags().Int64Var(&args.PartSize, "part-size", 5*1024*1024, "Multipart part size bytes")
	root.AddCommand(cp)
	moveArgs := s3core.Args{}
	mv := &cobra.Command{Use: "mv s3://source/key s3://target/key", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, values []string) error {
		var e error
		moveArgs.SourceBucket, moveArgs.SourceKey, e = remote(values[0], a.p.URIScheme, false)
		if e != nil {
			return e
		}
		moveArgs.Bucket, moveArgs.Key, e = remote(values[1], a.p.URIScheme, false)
		if e != nil {
			return e
		}
		return a.invoke(cmd, "s3_move_object", moveArgs)
	}}
	mv.Flags().BoolVar(&moveArgs.Overwrite, "overwrite", false, "Overwrite destination with approval")
	root.AddCommand(mv)
}
func (a *app) copyOne(cmd *cobra.Command, c *s3core.Client, source, destination string, args s3core.Args) error {
	srcRemote, dstRemote := a.isRemote(source), a.isRemote(destination)
	switch {
	case srcRemote && dstRemote:
		var e error
		args.SourceBucket, args.SourceKey, e = remote(source, a.p.URIScheme, false)
		if e != nil {
			return e
		}
		args.Bucket, args.Key, e = remote(destination, a.p.URIScheme, false)
		if e != nil {
			return e
		}
		_, e = c.Execute(cmd.Context(), "s3_copy_object", args, nil)
		return e
	case !srcRemote && dstRemote:
		var e error
		args.Bucket, args.Key, e = remote(destination, a.p.URIScheme, false)
		if e != nil {
			return e
		}
		// Confine the explicitly selected source to its directory while opening it.
		root, e := os.OpenRoot(filepath.Dir(source))
		if e != nil {
			return e
		}
		defer func() { _ = root.Close() }()
		in, e := root.Open(filepath.Base(source))
		if e != nil {
			return e
		}
		defer func() { _ = in.Close() }()
		info, e := in.Stat()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source must be a regular file")
		}
		_, e = c.UploadReader(cmd.Context(), args, in, nil)
		return e
	case srcRemote && !dstRemote:
		var e error
		args.Bucket, args.Key, e = remote(source, a.p.URIScheme, false)
		if e != nil {
			return e
		}
		path, e := filepath.Abs(destination)
		if e != nil {
			return e
		}
		if !args.Overwrite {
			if _, e = os.Lstat(path); e == nil {
				return fmt.Errorf("local destination already exists; use --overwrite --yes")
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		oldRoot := c.Config.FileRoot
		c.Config.FileRoot = filepath.Dir(path)
		defer func() { c.Config.FileRoot = oldRoot }()
		args.Path = filepath.Base(path)
		_, e = c.Execute(cmd.Context(), "s3_download_file", args, nil)
		return e
	default:
		return fmt.Errorf("cp requires at least one S3 URI")
	}
}
func (a *app) copyRecursive(cmd *cobra.Command, c *s3core.Client, source, destination string, args s3core.Args) error {
	if !a.isRemote(source) {
		if !a.isRemote(destination) {
			return fmt.Errorf("recursive cp requires an S3 URI")
		}
		info, e := os.Stat(source)
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return fmt.Errorf("recursive source must be a directory")
		}
		return filepath.WalkDir(source, func(path string, entry fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("recursive copy refuses symlinks")
			}
			relative, e := filepath.Rel(source, path)
			if e != nil {
				return e
			}
			return a.copyOne(cmd, c, path, strings.TrimSuffix(destination, "/")+"/"+filepath.ToSlash(relative), args)
		})
	}
	bucket, prefix, e := remote(source, a.p.URIScheme, false)
	if e != nil {
		return e
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	token := ""
	for {
		raw, e := c.Execute(cmd.Context(), "s3_list_objects", s3core.Args{Bucket: bucket, Prefix: prefix, Token: token}, nil)
		if e != nil {
			return e
		}
		page := raw.(map[string]any)
		for _, object := range page["objects"].([]map[string]any) {
			key := *object["key"].(*string)
			relative := strings.TrimPrefix(key, prefix)
			if relative == "" {
				continue
			}
			if !filepath.IsLocal(relative) || strings.Contains(relative, "\\") {
				return fmt.Errorf("unsafe local relative key")
			}
			for _, segment := range strings.Split(relative, "/") {
				if segment == ".." {
					return fmt.Errorf("unsafe '..' key")
				}
			}
			target := strings.TrimSuffix(destination, "/") + "/" + relative
			if !a.isRemote(destination) {
				path := filepath.Join(destination, filepath.FromSlash(relative))
				// os.Root confines directory creation even if an existing child is a symlink.
				if e = os.MkdirAll(destination, 0700); e != nil {
					return e
				}
				root, e := os.OpenRoot(destination)
				if e != nil {
					return e
				}
				e = root.MkdirAll(filepath.Dir(filepath.FromSlash(relative)), 0700)
				ce := root.Close()
				if e != nil {
					return e
				}
				if ce != nil {
					return ce
				}
				target = path
			}
			if e = a.copyOne(cmd, c, "s3://"+bucket+"/"+key, target, args); e != nil {
				return e
			}
		}
		next := page["continuation_token"].(string)
		if next == "" {
			return nil
		}
		if next == token {
			return fmt.Errorf("S3 repeated continuation token")
		}
		token = next
	}
}
