package build

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/lambdawalker/go.attestra.aws/artifacts"
)

func PackageLambda(binary, archive string) (err error) {
	in, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(archive), "lambda-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	z := zip.NewWriter(out)
	header := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	header.SetMode(0755)
	entry, err := z.CreateHeader(header)
	if err == nil {
		_, err = io.Copy(entry, in)
	}
	if e := z.Close(); err == nil {
		err = e
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	// Go uses replace-existing rename on Windows as well. Never expose a missing
	// archive between removing the old output and publishing the new one.
	return os.Rename(out.Name(), archive)
}

// LambdaNames includes reusable feature artifacts and the application admin.
var LambdaNames = func() []string {
	names := make([]string, 0, len(artifacts.Lambdas())+1)
	for _, artifact := range artifacts.Lambdas() {
		names = append(names, artifact.Name)
	}
	return append(names, "admin")
}()

type lambdaBuild struct {
	source string
	names  []string
}

func lambdaBuilds() []lambdaBuild {
	jobs := []lambdaBuild{}
	indexes := map[string]int{}
	commands := artifacts.Lambdas()
	commands = append(commands, artifacts.Lambda{Name: "admin", Package: "./cmd/admin"})
	for _, artifact := range commands {
		name, source := artifact.Name, artifact.Package
		if i, ok := indexes[source]; ok {
			jobs[i].names = append(jobs[i].names, name)
		} else {
			indexes[source] = len(jobs)
			jobs = append(jobs, lambdaBuild{source: source, names: []string{name}})
		}
	}
	return jobs
}

// Wait for running jobs to exit before returning, even when a build fails.
func runLambdaBuilds(jobs []lambdaBuild, workers int, build func(context.Context, lambdaBuild) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := make(chan lambdaBuild, len(jobs))
	for _, job := range jobs {
		queue <- job
	}
	close(queue)
	var wg sync.WaitGroup
	var once sync.Once
	var first error
	for n := 0; n < max(1, min(workers, len(jobs))); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				if ctx.Err() != nil {
					return
				}
				if err := build(ctx, job); err != nil {
					once.Do(func() { first = err; cancel() })
					return
				}
			}
		}()
	}
	wg.Wait()
	return first
}

func Lambdas(root string) error { return buildTargets(root, lambdaBuilds()) }
func buildTargets(root string, jobs []lambdaBuild) error {
	workers := min(4, max(1, runtime.GOMAXPROCS(0)/2))
	if raw := os.Getenv("ATTESTRA_BUILD_WORKERS"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 16 {
			return fmt.Errorf("ATTESTRA_BUILD_WORKERS must be between 1 and 16")
		}
		workers = n
	}
	parallel := max(1, min(4, runtime.GOMAXPROCS(0)/workers))
	return withBuildLock(root, func() error {
		return runLambdaBuilds(jobs, workers, func(ctx context.Context, job lambdaBuild) error {
			return cachedBuild(ctx, root, job, func(binary string) error {
				cmd := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-p", strconv.Itoa(parallel), "-trimpath", "-tags", "lambda.norpc", "-o", binary, job.source)
				cmd.Dir = root
				cmd.Env = buildEnvironment()
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("build %s: %w\n%s", job.source, err, output.String())
				}
				return nil
			})
		})
	})
}

func CheckArchives(root string) error {
	for _, job := range lambdaBuilds() {
		path := cachePath(root, job.source)
		b, e := os.ReadFile(path)
		var m cacheManifest
		if e != nil || json.Unmarshal(b, &m) != nil || !reusable(root, path, m.Desired, job.names) {
			return fmt.Errorf("archive integrity check failed for %s; restart deployment to rebuild", job.source)
		}
		desired, e := inputFingerprint(context.Background(), root, job.source)
		if e != nil {
			return e
		}
		if desired != m.Desired {
			return fmt.Errorf("build inputs changed for %s; rebuild before deployment", job.source)
		}
	}
	for _, name := range LambdaNames {
		path := filepath.Join(root, "dist", name+".zip")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("built archive %s is missing or empty; restart deployment to rebuild", name)
		}
	}
	return nil
}
