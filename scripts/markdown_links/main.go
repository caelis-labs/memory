package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

func main() {
	files, err := markdownFiles()
	if err != nil {
		fail(err)
	}
	errors := make([]string, 0)
	for _, file := range files {
		links, err := linksIn(file)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		for _, link := range links {
			line, target := link.line, link.target
			if externalOrAnchor(target) {
				continue
			}
			path := strings.SplitN(target, "#", 2)[0]
			if path == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(file), filepath.FromSlash(path)))
			info, err := os.Stat(resolved)
			if err != nil || info.IsDir() {
				errors = append(errors, fmt.Sprintf("%s:%d: missing Markdown target %q", file, line, target))
			}
		}
	}
	if len(errors) != 0 {
		sort.Strings(errors)
		for _, message := range errors {
			fmt.Fprintln(os.Stderr, message)
		}
		os.Exit(1)
	}
	fmt.Printf("documentation links passed (%d files checked)\n", len(files))
}

func markdownFiles() ([]string, error) {
	files := []string{"README.md"}
	err := filepath.WalkDir("docs", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk documentation: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

type link struct {
	line   int
	target string
}

func linksIn(path string) ([]link, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	links := make([]link, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		for _, match := range markdownLink.FindAllStringSubmatch(scanner.Text(), -1) {
			links = append(links, link{
				line:   line,
				target: strings.TrimSpace(strings.Trim(match[1], "<>")),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return links, nil
}

func externalOrAnchor(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(target, "#") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
