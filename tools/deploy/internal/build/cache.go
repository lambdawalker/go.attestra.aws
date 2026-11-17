package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
)

type cacheManifest struct {
	Version   int
	Desired   string
	Artifacts map[string]string
}

func digestFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func buildEnvironment() []string {
	return append(awsenv.WithoutCredentials(os.Environ()), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
}
func inputFingerprint(ctx context.Context, root, source string) (string, error) {
	h := sha256.New()
	io.WriteString(h, "attestra-lambda-recipe-v1:trimpath:lambda.norpc:zip-deflate-mode0755\n")
	run := func(args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, "go", args...)
		c.Dir = root
		c.Env = buildEnvironment()
		b, e := c.Output()
		if e != nil {
			return nil, fmt.Errorf("cache input discovery %s failed: %w", source, e)
		}
		return b, nil
	}
	version, e := run("version")
	if e != nil {
		return "", e
	}
	h.Write(version)
	env, e := run("env", "-json", "GOOS", "GOARCH", "GOARM64", "GOEXPERIMENT", "CGO_ENABLED", "GOFLAGS", "GOTOOLCHAIN", "GOVERSION", "GOROOT", "GOPATH", "GOPROXY", "GOSUMDB", "GOMOD", "GOWORK", "GOENV", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS")
	if e != nil {
		return "", e
	}
	h.Write(env)
	data, e := run("list", "-mod=readonly", "-deps", "-json", "-tags", "lambda.norpc", source)
	if e != nil {
		return "", e
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	files := map[string]bool{filepath.Join(root, "go.mod"): true, filepath.Join(root, "go.sum"): true}
	for {
		var p struct {
			Dir                                                                                                                 string
			Standard                                                                                                            bool
			GoFiles, CgoFiles, CFiles, CXXFiles, MFiles, HFiles, FFiles, SFiles, SwigFiles, SwigCXXFiles, SysoFiles, EmbedFiles []string
			Module                                                                                                              *struct {
				GoMod   string
				Replace *struct{ GoMod string }
			}
		}
		e = decoder.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
		if p.Standard {
			continue
		}
		for _, list := range [][]string{p.GoFiles, p.CgoFiles, p.CFiles, p.CXXFiles, p.MFiles, p.HFiles, p.FFiles, p.SFiles, p.SwigFiles, p.SwigCXXFiles, p.SysoFiles, p.EmbedFiles} {
			for _, file := range list {
				files[filepath.Join(p.Dir, file)] = true
			}
		}
		if p.Module != nil {
			if p.Module.GoMod != "" {
				files[p.Module.GoMod] = true
			}
			if p.Module.Replace != nil && p.Module.Replace.GoMod != "" {
				files[p.Module.Replace.GoMod] = true
			}
		}
	}
	names := make([]string, 0, len(files))
	for file := range files {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		sum, e := digestFile(file)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%s\x00%s\n", file, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func cachePath(root, source string) string {
	id := sha256.Sum256([]byte(source))
	return filepath.Join(root, ".deploy", "build-cache", fmt.Sprintf("%x.json", id))
}
func reusable(root, path, desired string, names []string) bool {
	b, e := os.ReadFile(path)
	if e != nil {
		return false
	}
	var m cacheManifest
	if json.Unmarshal(b, &m) != nil || m.Version != 1 || m.Desired != desired || len(m.Artifacts) != len(names) {
		return false
	}
	for _, name := range names {
		sum, e := digestFile(filepath.Join(root, "dist", name+".zip"))
		if e != nil || sum != m.Artifacts[name] {
			return false
		}
	}
	return true
}
func withBuildLock(root string, work func() error) error {
	dir := filepath.Join(root, ".deploy")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	path := filepath.Join(dir, "build.lock")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("build is locked; wait for the other writer or inspect .deploy/build.lock after a crash")
	}
	fmt.Fprintln(f, os.Getpid())
	f.Close()
	defer os.Remove(path)
	return work()
}
func cachedBuild(ctx context.Context, root string, job lambdaBuild, compile func(string) error) error {
	desired, e := inputFingerprint(ctx, root, job.source)
	if e != nil {
		return e
	}
	path := cachePath(root, job.source)
	if reusable(root, path, desired, job.names) {
		fmt.Printf("Reused %s\n", job.source)
		return nil
	}
	dir, e := os.MkdirTemp(filepath.Join(root, ".deploy"), "build-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "bootstrap")
	if e = compile(binary); e != nil {
		return e
	}
	after, e := inputFingerprint(ctx, root, job.source)
	if e != nil {
		return e
	}
	if after != desired {
		return errors.New("build inputs changed during compilation; retry from stable sources")
	}
	if e = os.MkdirAll(filepath.Join(root, "dist"), 0755); e != nil {
		return e
	}
	m := cacheManifest{Version: 1, Desired: desired, Artifacts: map[string]string{}}
	for _, name := range job.names {
		if e = ctx.Err(); e != nil {
			return e
		}
		archive := filepath.Join(root, "dist", name+".zip")
		if e = PackageLambda(binary, archive); e != nil {
			return e
		}
		m.Artifacts[name], e = digestFile(archive)
		if e != nil {
			return e
		}
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), "manifest-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	fmt.Printf("Built %s\n", job.source)
	return nil
}

func Shared(root string) error {
	return buildTargets(root, []lambdaBuild{{source: "./cmd/index", names: []string{"index"}}, {source: "./cmd/admin", names: []string{"admin"}}})
}
