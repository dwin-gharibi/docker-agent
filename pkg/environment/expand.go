package environment

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/docker/docker-agent/pkg/path"
)

func ExpandAll(ctx context.Context, values []string, env Provider) ([]string, error) {
	var expandedEnv []string

	for _, value := range values {
		expanded, err := Expand(ctx, value, env)
		if err != nil {
			return nil, err
		}

		expandedEnv = append(expandedEnv, expanded)
	}

	return expandedEnv, nil
}

func Expand(ctx context.Context, value string, env Provider) (string, error) {
	var err error

	// Accept the JS-template `${env.VAR}` form as an alias for `${VAR}` so the
	// syntax used in prompts/headers also resolves here (issue #2615).
	value = path.NormalizeEnvRefs(value)

	expanded := os.Expand(value, func(name string) string {
		v, found := env.Get(ctx, name)
		if !found {
			err = fmt.Errorf("environment variable %q not set", name)
		}
		return v
	})
	if err != nil {
		return "", err
	}

	return expanded, nil
}

// Refs returns the names of the environment variables referenced by value
// using the same ${env.X} / ${X} syntax Expand resolves, in first-seen order
// and de-duplicated. It lets callers discover which variables a config field
// requires (e.g. for an up-front "missing env var" check) without resolving
// them. A value with no references yields nil.
func Refs(value string) []string {
	value = path.NormalizeEnvRefs(value)

	var names []string
	seen := map[string]bool{}
	os.Expand(value, func(name string) string {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		return ""
	})
	return names
}

func ToValues(envMap map[string]string) []string {
	var values []string
	for k, v := range envMap {
		values = append(values, fmt.Sprintf("%s=%s", k, v))
	}
	slices.Sort(values)
	return values
}
