package project

import (
	"embed"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/deploy"
	"github.com/lambdawalker/go.attestra.aws/app"
	"os"
)

//go:embed manifest.example.json
var files embed.FS

func Init(path string) error {
	data, e := files.ReadFile("manifest.example.json")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
func Load(path string) (deploy.Manifest, error) {
	f, e := os.Open(path)
	if e != nil {
		return deploy.Manifest{}, e
	}
	defer f.Close()
	m, e := deploy.Parse(f)
	if e != nil {
		return m, e
	}
	if m.Application != "attestra" {
		return m, fmt.Errorf("manifest application must be attestra")
	}
	a, e := app.Compose(auth.Module{})
	if e != nil {
		return m, e
	}
	engine := deploy.Engine{Manifest: m}
	if e = engine.Use(a); e != nil {
		return m, e
	}
	return engine.Manifest, nil
}
