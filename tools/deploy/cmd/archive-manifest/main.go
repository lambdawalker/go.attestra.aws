// Command archive-manifest prints the application Lambda archive inventory.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/build"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(build.LambdaNames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
