package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"net/http"

	"github.com/google/go-github/v55/github"
	"github.com/hashicorp/go-version"
	"path/filepath"
)

func main() {
	versionFrom := flag.String("from", "", "Lower Pulumi version to bisect from")
	versionTo := flag.String("to", "", "Upper Pulumi version to bisect to")
	cmd := flag.String("cmd", "", "Command to check if a given Pulumi version is bad")
	flag.Parse()

	v1, err := version.NewVersion(*versionFrom)
	if err != nil {
		log.Fatal(err)
	}

	v2, err := version.NewVersion(*versionTo)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	client := github.NewClient(nil)
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		client = client.WithAuthToken(t)
	}

	// Find GitHub releases for pulumi/pulumi repository
	releases, err := listReleasedVersions(ctx, client, "pulumi", "pulumi")
	if err != nil {
		log.Fatal(err)
	}

	seen := map[string]struct{}{}
	releaseRange := []*version.Version{}
	for _, r := range releases {
		if _, ok := seen[r.String()]; ok {
			continue
		}
		if r.LessThanOrEqual(v2) && r.GreaterThanOrEqual(v1) {
			releaseRange = append(releaseRange, r)
		}
		seen[r.String()] = struct{}{}
	}

	sort.Slice(releaseRange, func(i, j int) bool {
		return releaseRange[i].LessThan(releaseRange[j])
	})

	if len(releaseRange) == 0 {
		fmt.Println(">>> empty release range")
		return
	}

	fmt.Printf(">>> checking %s..%s\n", releaseRange[0].String(), releaseRange[len(releaseRange)-1].String())

	v := bisectFirstBad(releaseRange, func(v *version.Version) bool {
		return badRelease(*cmd, v)
	})
	if v != nil {
		fmt.Println(">>> First bad release found: ", (*v).String())
	} else {
		fmt.Println(">>> No bad releases found")
	}
}

func badRelease(cmd string, v *version.Version) bool {
	p := downloadPulumi(v)
	fmt.Printf("=== Running %s with %s\n", cmd, p)
	command := exec.Command(cmd)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	path := os.Getenv("PATH")
	if path != "" {
		path = fmt.Sprintf("%s:%s", p, path)
	} else {
		path = p
	}
	env := append(os.Environ(), fmt.Sprintf("PATH=%s", path))
	command.Env = env
	command.Run()
	if command.ProcessState.ExitCode() != 0 {
		return true
	}
	return false
}

func downloadPulumiDownloader() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	d := filepath.Join(cache, ".pulumi-bisect", "installer")
	p := filepath.Join(d, "install-pulumi.sh")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	fmt.Printf("=== Installing Pulumi installer\n")
	code := httpGet("https://get.pulumi.com")
	if err := os.MkdirAll(d, 0755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(code), 0755); err != nil {
		log.Fatal(err)
	}
	return p
}

func downloadPulumi(v *version.Version) string {
	cache, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	h := filepath.Join(cache, ".pulumi-bisect", "pulumi", v.String())
	d := filepath.Join(h, ".pulumi", "bin")
	if _, err = os.Stat(d); err == nil {
		return d
	}
	if err := os.MkdirAll(d, 0755); err != nil {
		log.Fatal(err)
	}
	installer := downloadPulumiDownloader()
	cmd := exec.Command(installer, "--version", strings.TrimPrefix(v.String(), "v"))
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", h))
	fmt.Printf("=== Installing Pulumi %s\n", v.String())
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	return d
}

func httpGet(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func bisectFirstBad[T any](candidates []T, bad func(T) bool) *T {
	for {
		if len(candidates) == 0 {
			return nil
		}
		if len(candidates) == 1 {
			c := candidates[0]
			if bad(c) {
				return &c
			} else {
				return nil
			}
		}
		mid := len(candidates) / 2
		if bad(candidates[mid]) {
			candidates = candidates[:mid]
		} else {
			candidates = candidates[mid+1:]
		}
	}
}

func listReleasedVersions(ctx context.Context, client *github.Client, owner, repo string) ([]*version.Version, error) {
	perPage := 25
	page := 0
	out := []*version.Version{}
	for {
		releases, _, err := client.Repositories.ListReleases(ctx, owner, repo, &github.ListOptions{
			Page:    page,
			PerPage: perPage,
		})
		if err != nil && strings.Contains(err.Error(), "rate limit") {
			time.Sleep(1 * time.Second)
			page--
		} else if err != nil {
			return nil, err
		}
		for _, r := range releases {
			if r.GetTagName() != "" {
				if v, err := version.NewVersion(r.GetTagName()); err == nil {
					out = append(out, v)
				}
			}
		}
		if len(releases) == 0 {
			break
		}
		page++
	}
	return out, nil
}
